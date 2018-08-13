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

	conn, err := p.Acquire()
	if err != nil {
		t.Errorf("Acquire %s", err)
	}
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
	count := 1000
	p := New(builder, MaxNum(count))

	var closers []io.Closer
	for i := 0; i < count; i++ {
		obj, err := p.Acquire()
		if err != nil {
			t.Error(err)
		}
		closers = append(closers, obj)
	}

	if p.Status().Active != count {
		t.Errorf("active != %d after get %d times", count, count)
	}

	for _, conn := range closers {
		p.Release(conn, false)
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
			obj, err := p.Acquire()
			if err != nil {
				t.Error(err)
			}
			cc <- obj
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
		p.Release(<-cc, false)
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
	p := New(builder, MaxNum(1000))
	defer p.Close()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			conn, err := p.Acquire()
			if err != nil {
				b.Error(err)
			}
			p.Release(conn, false)
		}
	})
}

func BenchmarkDo(b *testing.B) {
	p := New(builder, MaxNum(20))
	defer p.Close()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			p.Do(func(obj io.Closer) error {
				_, _ = obj.(*mockResource)
				return nil
			})
		}
	})
}
