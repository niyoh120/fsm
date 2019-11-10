// Simple Finite State Machine for Go
package fsm

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
)

type Event int

type TransMap map[int]int

type Transition struct {
	Event  Event
	Map    TransMap
	Before func()
	After  func()
}

func (t *Transition) String() string {
	return fmt.Sprintf("Transition[event:%d,map:%v]", t.Event, t.Map)
}

type State struct {
	Index   int
	OnEnter func()
	OnExit  func()
}

func (s *State) String() string {
	return fmt.Sprintf("State[index:%d]", s.Index)
}

type Error struct {
	Name       string
	StateIndex int
	Event      Event

	msg string
}

func (e *Error) Error() string {
	return e.msg
}

func makeError(name string, s int, e Event, msg string) *Error {
	return &Error{
		Name:       name,
		StateIndex: s,
		Event:      e,
		msg:        fmt.Sprintf("%s(State[index:%d], Event: %d) %s", name, s, e, msg),
	}
}

type FSM struct {
	locker      sync.Locker
	rwLocker    sync.RWMutex
	flag        uint32
	name        string
	stateMap    map[int]*State
	transitions []*Transition
	current     int
	queue       chan Event
}

func (f *FSM) String() string {
	return fmt.Sprintf("FSM[%s]", f.name)
}

func (f *FSM) Lock() {
	f.locker.Lock()
}

func (f *FSM) Unlock() {
	f.locker.Unlock()
}

func (f *FSM) LockEvent(e Event) error {
	defer f.Unlock()
	f.Lock()
	return f.Event(e)
}

func (f *FSM) Event(e Event) error {
	defer f.restoreFlag()
	if f.setCheckFlag() {
		return f.tryEnqueue(e)
	}
	if err := f.event(e); err != nil {
		return err
	}
	for {
		select {
		case e := <-f.queue:
			if err := f.event(e); err != nil {
				f.dropEventInQueue()
				return err
			}
		default:
			return nil
		}
	}
}

func (f *FSM) event(e Event) error {
	f.rwLocker.RLock()
	current := f.current
	t, ok := f.getTransition(e)
	f.rwLocker.RUnlock()

	if !ok {
		return makeError(f.String(), current, e, "event invalid in current state")
	}

	next := t.Map[current]
	c, n := f.stateMap[current], f.stateMap[next]

	if t.Before != nil {
		t.Before()
	}

	if current != next {
		if c.OnExit != nil {
			c.OnExit()
		}
	}

	f.rwLocker.Lock()
	f.current = next
	f.rwLocker.Unlock()

	if current != next {
		if n.OnEnter != nil {
			n.OnEnter()
		}
	}
	if t.After != nil {
		t.After()
	}

	return nil
}

func (f *FSM) tryEnqueue(e Event) error {
	select {
	case f.queue <- e:
		return nil
	default:
		return makeError(f.name, f.CurrentStateIndex(), e, "fsm event queue is full")
	}
}

func (f *FSM) dropEventInQueue() {
	for {
		select {
		case <-f.queue:
		default:
			return
		}
	}
}

func (f *FSM) setCheckFlag() bool {
	flag := atomic.SwapUint32(&f.flag, 1)
	return flag == 1
}

func (f *FSM) restoreFlag() {
	atomic.StoreUint32(&f.flag, 0)
}

func (f *FSM) Can(e Event) (ok bool) {
	defer f.rwLocker.RUnlock()
	f.rwLocker.RLock()
	_, ok = f.getTransition(e)
	return
}

func (f *FSM) CurrentStateIndex() int {
	defer f.rwLocker.RUnlock()
	f.rwLocker.RLock()
	return f.current
}

func (f *FSM) getTransition(e Event) (t *Transition, ok bool) {
	for _, t = range f.transitions {
		_, ok = t.Map[f.current]
		if !ok {
			continue
		}
		if t.Event == e {
			return t, true
		}
	}
	return nil, false
}

type Builder struct {
	err         error
	name        string
	queueSize   int
	locker      sync.Locker
	initState   *State
	stateMap    map[int]*State
	transitions []*Transition
}

func New() *Builder {
	return &Builder{
		queueSize:   12,
		stateMap:    map[int]*State{},
		transitions: []*Transition{},
	}
}

func (b *Builder) Name(name string) *Builder {
	if b.err != nil {
		return b
	}
	b.name = name
	return b
}

func (b *Builder) QueueSize(size int) *Builder {
	b.queueSize = size
	return b
}

func (b *Builder) WithLocker(locker sync.Locker) *Builder {
	b.locker = locker
	return b
}

func (b *Builder) InitState(s *State) *Builder {
	if b.err != nil {
		return b
	}
	if _, ok := b.stateMap[s.Index]; ok {
		b.err = fmt.Errorf("try add existed %s", s)
		return b
	}
	b.stateMap[s.Index] = s
	b.initState = s
	return b
}

func (b *Builder) AddStates(states ...*State) *Builder {
	if b.err != nil {
		return b
	}
	for _, s := range states {
		if _, ok := b.stateMap[s.Index]; ok {
			b.err = fmt.Errorf("try add existed %s", s)
			return b
		}
		b.stateMap[s.Index] = s
	}
	return b
}

func (b *Builder) AddTransitions(transitions ...*Transition) *Builder {
	if b.err != nil {
		return b
	}
	b.transitions = append(b.transitions, transitions...)
	return b
}

func (b *Builder) Build() (*FSM, error) {
	for _, t := range b.transitions {
		ok := b.checkTransition(t)
		if !ok {
			b.err = fmt.Errorf("invalid transition %s", t)
			break
		}
	}

	if b.initState == nil {
		b.err = errors.New("no init state")
	}

	if b.err != nil {
		return nil, b.err
	}

	f := &FSM{
		name:        b.name,
		stateMap:    b.stateMap,
		transitions: b.transitions,
		current:     b.initState.Index,
		queue:       make(chan Event, b.queueSize),
	}
	if f.name == "" {
		f.name = fmt.Sprintf("%p", f)
	}
	if b.locker == nil {
		f.locker = &sync.Mutex{}
	} else {
		f.locker = b.locker
	}

	return f, nil
}

func (b *Builder) MustBuild() *FSM {
	fsm, err := b.Build()
	if err != nil {
		panic(err)
	}
	return fsm
}

func (b *Builder) checkTransition(t *Transition) (ok bool) {
	if t.Map == nil && len(t.Map) < 1 {
		return
	}
	for s, d := range t.Map {
		_, ok = b.stateMap[s]
		_, ok = b.stateMap[d]
		if !ok {
			return
		}
	}
	return true
}
