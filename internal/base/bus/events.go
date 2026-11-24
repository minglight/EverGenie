package bus

import (
	"context"
	"reflect"
	"strings"
	"sync"

	"github.com/minglight/evergenie/sdk"
)

// Each subscription owns an unbounded queue; senders never wait for its reader.
type queue[T any] struct {
	mu    sync.Mutex
	items []T
	wake  chan struct{}
	out   chan T
}

func newQueue[T any]() *queue[T] { return &queue[T]{wake: make(chan struct{}, 1), out: make(chan T)} }
func (q *queue[T]) push(v T) int {
	q.mu.Lock()
	q.items = append(q.items, v)
	n := len(q.items)
	q.mu.Unlock()
	select {
	case q.wake <- struct{}{}:
	default:
	}
	return n
}
func (q *queue[T]) run(ctx, service context.Context) {
	defer close(q.out)
	for {
		q.mu.Lock()
		if len(q.items) == 0 {
			q.mu.Unlock()
			select {
			case <-ctx.Done():
				return
			case <-service.Done():
				return
			case <-q.wake:
			}
			continue
		}
		value := q.items[0]
		var zero T
		q.items[0] = zero
		q.items = q.items[1:]
		q.mu.Unlock()
		select {
		case <-ctx.Done():
			return
		case <-service.Done():
			return
		case q.out <- value:
		}
	}
}

type eventSub struct {
	topics []string
	q      *queue[Event]
}
type watchSub struct {
	to, id string
	q      *queue[*sdk.BusWatchResponse]
}

func (m *Module) Subscribe(ctx context.Context, topics ...string) (<-chan Event, error) {
	m.life.RLock()
	defer m.life.RUnlock()
	if err := m.ready(ctx); err != nil {
		return nil, err
	}
	s := &eventSub{topics: append([]string(nil), topics...), q: newQueue[Event]()}
	m.mu.Lock()
	m.subs[s] = struct{}{}
	m.mu.Unlock()
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		defer m.recoverWorker()
		defer func() { m.mu.Lock(); delete(m.subs, s); m.mu.Unlock() }()
		s.q.run(ctx, m.ctx)
	}()
	return s.q.out, nil
}
func (m *Module) Watch(ctx context.Context, to, id string) (<-chan *sdk.BusWatchResponse, error) {
	m.life.RLock()
	defer m.life.RUnlock()
	if err := m.ready(ctx); err != nil {
		return nil, err
	}
	s := &watchSub{to: to, id: id, q: newQueue[*sdk.BusWatchResponse]()}
	m.mu.Lock()
	m.watches[s] = struct{}{}
	m.mu.Unlock()
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		defer m.recoverWorker()
		defer func() { m.mu.Lock(); delete(m.watches, s); m.mu.Unlock() }()
		s.q.run(ctx, m.ctx)
	}()
	return s.q.out, nil
}
func match(topics []string, topic string) bool {
	if len(topics) == 0 {
		return true
	}
	for _, pattern := range topics {
		if pattern == topic || (strings.HasSuffix(pattern, "*") && strings.HasPrefix(topic, strings.TrimSuffix(pattern, "*"))) {
			return true
		}
	}
	return false
}
func copyValue(v reflect.Value) reflect.Value {
	switch v.Kind() {
	case reflect.Interface:
		if v.IsNil() {
			return v
		}
		o := reflect.New(v.Type()).Elem()
		o.Set(copyValue(v.Elem()))
		return o
	case reflect.Map:
		if v.IsNil() {
			return v
		}
		o := reflect.MakeMapWithSize(v.Type(), v.Len())
		it := v.MapRange()
		for it.Next() {
			o.SetMapIndex(it.Key(), copyValue(it.Value()))
		}
		return o
	case reflect.Slice:
		if v.IsNil() {
			return v
		}
		o := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		for i := 0; i < v.Len(); i++ {
			o.Index(i).Set(copyValue(v.Index(i)))
		}
		return o
	default:
		return v
	}
}
func clonePayload(p map[string]any) map[string]any {
	if p == nil {
		return nil
	}
	return copyValue(reflect.ValueOf(p)).Interface().(map[string]any)
}
func (m *Module) Publish(ctx context.Context, topic string, payload map[string]any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for s := range m.subs {
		if match(s.topics, topic) {
			n := s.q.push(&sdk.BusSubscribeResponse{Topic: topic, Payload: clonePayload(payload), At: m.d.Clock.Now()})
			if n == 1001 && m.log != nil {
				m.log.Warn("subscriber queue exceeds 1000")
			}
		}
	}
	return nil
}
func (m *Module) changed(e Envelope) {
	m.mu.Lock()
	for s := range m.watches {
		if (s.to == "" || s.to == e.To) && (s.id == "" || s.id == e.ID) {
			v := clone(e)
			step := v.History[len(v.History)-1]
			n := s.q.push(&sdk.BusWatchResponse{Envelope: v, Step: &step})
			if n == 1001 && m.log != nil {
				m.log.Warn("watch queue exceeds 1000")
			}
		}
	}
	m.mu.Unlock()
	if err := m.Publish(context.Background(), "bus.envelope.state", map[string]any{"id": e.ID, "from": e.From, "to": e.To, "kind": string(e.Kind), "route": string(e.Route), "notice": e.Notice, "state": string(e.Lifecycle), "reason": e.Reason}); err != nil {
		m.log.Warn("state publish failed")
	}
}
