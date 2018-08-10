package pool

import (
	"container/list"
	"errors"
	"io"
	"sync"
	"time"
)

var (
	ErrPoolHasClosed = errors.New("pool has closed")
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
	close(p.stopCh)

	for e := p.idle.Front(); e != nil; e = e.Next() {
		if err := e.Value.(*idle).Close(); err != nil {
			p.opt.logger.Printf("close pool failed %s", err.Error())
		}
	}

	p.active -= p.idle.Len()
	p.idle.Init()
	if p.cond != nil {
		p.cond.Broadcast()
	}
	p.lock.Unlock()

	return nil
}

var idlePool = &sync.Pool{
	New: func() interface{} {
		return &idle{}
	},
}

func (p *Pool) Release(closer io.Closer, forceClose bool) error {
	if _, is := closer.(*errCloser); is {
		return nil
	}

	if p.isClosed() || forceClose {
		p.active--
		return closer.Close()
	}

	now := time.Now()
	p.lock.Lock()
	if p.opt.MaxNum != 0 && (p.active > p.opt.MaxNum || p.idle.Len() >= p.opt.MaxNum) {
		p.active--
		p.lock.Unlock()
		return closer.Close()
	}

	i := idlePool.Get().(*idle)
	i.Closer = closer
	i.activeTime = now
	// p.idle.PushFront(&idle{Closer: closer, activeTime: now})
	p.idle.PushFront(i)
	p.wakeup()
	p.lock.Unlock()

	return nil
}

func (p *Pool) acquire() (closer io.Closer, err error) {
	if p.isClosed() {
		return nil, ErrPoolHasClosed
	}

	var (
		now     = time.Now()
		element *list.Element
	)

	p.lock.Lock()
	p.prune(now)
	if element = p.idle.Front(); element != nil {
		i := p.idle.Remove(element).(*idle)
		closer = i.Closer
		p.lock.Unlock()
		idlePool.Put(i)
		return closer, nil
	}

	// not enough
	if p.opt.MaxNum == 0 || p.active < p.opt.MaxNum {
		p.active++
		if closer, err = p.factory(); err != nil {
			p.active--
		}
		p.lock.Unlock()
		return closer, err
	}

	for {
		p.wait()
		if p.isClosed() {
			p.lock.Unlock()
			return nil, ErrPoolHasClosed
		}

		if element = p.idle.Front(); element != nil {
			i := p.idle.Remove(element).(*idle)
			closer = i.Closer
			p.lock.Unlock()
			idlePool.Put(i)
			return closer, nil
		}
	}
}

func (p *Pool) Acquire() io.Closer {
	closer, err := p.acquire()
	if err != nil {
		return &errCloser{error: err}
	}
	return closer
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

func (p *Pool) Do(f func(obj io.Closer) error) error {
	obj, err := p.acquire()
	if err != nil {
		return err
	}
	err = f(obj)

	p.Release(obj, err != nil)
	return err
}
