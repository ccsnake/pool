package pool

import (
	"io"
	"sync"
	"sync/atomic"
	"testing"
)

type mockResource struct {
}

func (mr *mockResource) Close() error {
	return nil
}

func builder() (io.Closer, error) {
	return &mockResource{}, nil
}

func TestGet(t *testing.T) {
	p := New(builder, MaxNum(2))

	conn := p.Acquire()
	if _, is := conn.(*mockResource); !is {
		t.Error("Acquire err")
	}

	if p.Status().Active != 1 {
		t.Error("active != 1")
	}

	if err := conn.Close(); err != nil {
		t.Error(err)
	}

	if p.Status().Active != 1 {
		t.Error("active != 1 after close")
	}
}

func TestClose(t *testing.T) {
	cap := 1000
	p := New(builder, MaxNum(cap))

	var conns []io.Closer
	for i := 0; i < cap; i++ {
		conns = append(conns, p.Acquire())
	}

	if p.Status().Active != cap {
		t.Errorf("active != %d after get %d times", cap, cap)
	}

	for _, conn := range conns {
		p.Release(conn, nil)
	}

	if err := p.Close(); err != nil {
		t.Error(err)
	}

	if p.Status().Active != 0 {
		t.Error("active != 0 after close")
	}
}

func TestConnReuse(t *testing.T) {
	cap := 1000

	var dc int32

	nbuilder := func() (io.Closer, error) {
		atomic.AddInt32(&dc, 1)
		return builder()
	}

	p := New(nbuilder, MaxNum(cap))

	var wg sync.WaitGroup
	cc := make(chan io.Closer, cap)
	for j := 0; j < cap; j++ {
		wg.Add(1)
		go func() {
			cc <- p.Acquire()
			wg.Done()
		}()
	}
	wg.Wait()

	if n := int(atomic.LoadInt32(&dc)); n != cap {
		t.Errorf("dialcount expect %d but %d after get", cap, n)
	}

	if n := p.Status().Active; n != cap {
		t.Errorf("active expect %d but %d after get", cap, n)
	}

	for j := 0; j < cap; j++ {
		p.Release(<-cc, nil)
	}

	if n := p.Status().Idle; n != cap {
		t.Errorf("idle expect %d but %d after get close", cap, n)
	}

	if n := p.Status().Active; n != cap {
		t.Errorf("active expect %d but %d after get close", cap, n)
	}

	if err := p.Close(); err != nil {
		t.Error(err)
	}

	if p.Status().Active != 0 {
		t.Error("active != 0 after close")
	}

	if p.Status().Idle != 0 {
		t.Error("idle != 0 after close")
	}
}

func BenchmarkGet(b *testing.B) {
	cap := 1000
	p := New(builder, MaxNum(cap))
	defer p.Close()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			conn := p.Acquire()
			p.Release(conn, nil)
		}
	})
}
