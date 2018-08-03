package pool

import (
	"container/list"
	"errors"
	"io"
	"sync"
	"time"
)

var (
	ErrPoolHasClosed = errors.New("connection Pool is closed")
)

type FactoryFunc func() (io.Closer, error)

type errCloser struct {
	error
}

func (ec *errCloser) Close() error {
	return ec.error
}

type idle struct {
	activeTime time.Time
	io.Closer
}

type Pool struct {
	opt  Options
	addr string

	factory FactoryFunc

	cond *sync.Cond
	lock sync.Mutex
	idle *list.List

	active int
	stopCh chan struct{}
}

func New(factory FactoryFunc, opts ...Option) *Pool {
	options := newOptions(opts...)
	p := &Pool{
		factory: factory,
		opt:     options,
		idle:    list.New(),
		stopCh:  make(chan struct{}),
	}

	p.lock.Lock()
	for i := 0; i < p.opt.InitNum; i++ {
		closer, err := p.factory()
		if err != nil {
			p.opt.logger.Printf("pool init resource failed %s", err.Error())
			continue
		}
		p.active++
		p.idle.PushFront(&idle{Closer: closer, activeTime: time.Now()})
	}
	p.lock.Unlock()

	return p
}

func (p *Pool) Close() error {
	p.lock.Lock()
	defer p.lock.Unlock()

	close(p.stopCh)

	for e := p.idle.Front(); e != nil; e = e.Next() {
		if err := e.Value.(*idle).Close(); err != nil {
			p.opt.logger.Printf("Pool close failed for close timeout connection:%s", err.Error())
		}
	}

	p.active -= p.idle.Len()
	p.idle.Init()
	if p.cond != nil {
		p.cond.Broadcast()
	}

	return nil
}

func (p *Pool) Release(conn io.Closer, err error) error {
	if ec, is := conn.(*errCloser); is {
		return ec
	}

	if p.isClosed() || err != nil {
		p.active--
		return conn.Close()
	}

	now := time.Now()
	p.lock.Lock()
	if p.opt.MaxNum != 0 && (p.active > p.opt.MaxNum || p.idle.Len() >= p.opt.MaxNum) {
		p.active--
		p.lock.Unlock()
		return conn.Close()
	}

	p.idle.PushFront(&idle{Closer: conn, activeTime: now})
	p.wakeup()
	p.lock.Unlock()

	return nil
}

func (p *Pool) Acquire() (closer io.Closer) {
	if p.isClosed() {
		return &errCloser{error: ErrPoolHasClosed}
	}

	var (
		now     = time.Now()
		element *list.Element
		err     error
	)
	p.lock.Lock()
	p.prune(now)
	if element = p.idle.Front(); element != nil {
		closer = p.idle.Remove(element).(*idle).Closer
		goto END
	}

	// not enough
	if p.opt.MaxNum == 0 || p.active < p.opt.MaxNum {
		p.active++
		closer, err = p.factory()
		if err != nil {
			p.active--
			closer = &errCloser{error: err}
		}

		goto END
	}

WAIT:
	p.wait()
	if p.isClosed() {
		closer = &errCloser{error: ErrPoolHasClosed}
		goto END
	}

	element = p.idle.Front()
	if element == nil {
		goto WAIT
	}

	closer = p.idle.Remove(element).(*idle).Closer

END:
	p.lock.Unlock()
	return
}

func (p *Pool) wakeup() {
	if p.cond == nil {
		p.cond = sync.NewCond(&p.lock)
	}
	p.cond.Signal()
}

func (p *Pool) wait() {
	if p.cond == nil {
		p.cond = sync.NewCond(&p.lock)
	}
	p.cond.Wait()
}

func (p *Pool) prune(now time.Time) {
	// prune timeout connections
	for element := p.idle.Back(); element != nil && p.idle.Len() > p.opt.InitNum; element = element.Prev() {
		ic := element.Value.(*idle)
		if ic.activeTime.Add(p.opt.MaxIdleDuration).Sub(now) > 0 {
			break
		}
		p.idle.Remove(element)
		if err := ic.Closer.Close(); err != nil {
			p.opt.logger.Printf("prune failed for close timeout resource:%s", err.Error())
		}
		p.active--
	}
}

func (p *Pool) isClosed() bool {
	select {
	case <-p.stopCh:
		return true
	default:
		return false
	}
}

type Status struct {
	Active int
	Idle   int
}

func (p *Pool) Status() Status {
	p.lock.Lock()
	s := Status{
		Active: p.active,
		Idle:   p.idle.Len(),
	}
	p.lock.Unlock()
	return s
}
