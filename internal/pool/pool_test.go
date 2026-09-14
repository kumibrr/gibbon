package pool_test

import (
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kumibrr/gibbon/internal/pool"
)

func TestMapOrderedAndBounded(t *testing.T) {
	var active, peak int32
	got := pool.Map([]int{1, 2, 3, 4, 5, 6}, 2, func(i int) int {
		n := atomic.AddInt32(&active, 1)
		for {
			p := atomic.LoadInt32(&peak)
			if n <= p || atomic.CompareAndSwapInt32(&peak, p, n) {
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
		atomic.AddInt32(&active, -1)
		return i * 10
	})
	if !reflect.DeepEqual(got, []int{10, 20, 30, 40, 50, 60}) {
		t.Fatalf("%v", got)
	}
	if peak > 2 {
		t.Fatalf("peak %d", peak)
	}
	if r := pool.Map([]int{}, 0, func(i int) int { return i }); len(r) != 0 {
		t.Fatal("empty")
	}
	if r := pool.Map([]int{7}, 0, func(i int) int { return i }); r[0] != 7 {
		t.Fatal("workers<1")
	}
}
