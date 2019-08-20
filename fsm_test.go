package fsm

import (
	"github.com/stretchr/testify/assert"
	"strings"
	"testing"
)

func TestBuilder(t *testing.T) {
	var (
		ass = assert.New(t)
		err error
	)

	_, err = New().Build()
	ass.True(err != nil)

	_, err = New().
		InitState(&State{
			Index: 1,
		}).AddTransitions(&Transition{
		Event: 0,
		Map:   nil,
	}).Build()
	ass.True(err != nil)

	_, err = New().
		InitState(&State{
			Index: 1,
		}).AddTransitions(&Transition{
		Event: 0,
		Map: TransMap{
			1: 2,
		},
	}).Build()
	ass.True(err != nil)
}

func TestFSM(t *testing.T) {
	const (
		_ = iota
		testS1
		testS2
		testS3
	)

	const (
		_ = iota
		testE1
		testE2
		testE3
		testE4
	)

	var (
		ass    = assert.New(t)
		fsm    *FSM
		err    error
		result strings.Builder
	)

	done := make(chan struct{})

	s1 := &State{
		Index: testS1,
		OnEnter: func() {
			ass.Equal(testS1, fsm.CurrentStateIndex())
			result.WriteString("A")
		},
	}
	s2 := &State{
		Index: testS2,
		OnExit: func() {
			ass.Equal(testS2, fsm.CurrentStateIndex())
			result.WriteString("B")
		},
	}
	s3 := &State{
		Index: testS3,
		OnEnter: func() {
			ass.Equal(testS3, fsm.CurrentStateIndex())
			result.WriteString("C")
		},
		OnExit: func() {
			ass.Equal(testS3, fsm.CurrentStateIndex())
			result.WriteString("D")
		},
	}

	t1 := &Transition{
		Event: testE1,
		Map:   TransMap{testS1: testS2, testS2: testS3, testS3: testS1},
		Before: func() {
			result.WriteString("E")
		},
		After: nil,
	}
	t2 := &Transition{
		Event:  testE2,
		Map:    TransMap{testS1: testS3, testS3: testS2, testS2: testS1},
		Before: nil,
		After: func() {
			result.WriteString("F")
		},
	}
	t3 := &Transition{
		Event: testE3,
		Map:   TransMap{testS1: testS1},
		Before: func() {
			result.WriteString("G")
			go func() {
				defer fsm.Unlock()
				fsm.Lock()
				err = fsm.Event(testE1)
				ass.NoError(err)
				close(done)
			}()
			ass.True(fsm.Can(testE3))
		},
	}

	fsm = New().
		Name("test").
		InitState(s1).
		AddStates(s2, s3).
		AddTransitions(t1, t2, t3).
		MustBuild()

	// test init state
	ass.Equal(fsm.name, "test")
	ass.Equal(testS1, fsm.CurrentStateIndex())
	ass.Equal("", result.String())

	result.Reset()

	// S1->E1->S2
	fsm.Lock()
	err = fsm.Event(testE1)
	fsm.Unlock()

	ass.NoError(err)
	ass.Equal(testS2, fsm.CurrentStateIndex())
	ass.Equal("E", result.String())

	result.Reset()

	// S2->E2->S1
	fsm.Lock()
	err = fsm.Event(testE2)
	fsm.Unlock()

	ass.NoError(err)
	ass.Equal(testS1, fsm.CurrentStateIndex())
	ass.Equal("BAF", result.String())

	result.Reset()

	// S1->E3->S1->E1->S2
	fsm.Lock()
	err = fsm.Event(testE3)
	fsm.Unlock()
	ass.NoError(err)
	<-done
	ass.Equal(testS2, fsm.CurrentStateIndex())
	ass.Equal("GE", result.String())

	// unknown event
	fsm.Lock()
	err = fsm.Event(testE4)
	fsm.Unlock()
	v, ok := err.(*Error)
	ass.True(ok)
	ass.NotNil(v)
	ass.Equal(testS2, fsm.CurrentStateIndex())
}

func TestDebug(t *testing.T) {
	ass := assert.New(t)

	const (
		_ = iota
		testS1
		testS2
	)

	const (
		_ = iota
		testE1
	)

	var fsm *FSM
	s1 := &State{
		Index: testS1,
		OnExit: func() {
			err := fsm.Event(testE1)
			v, ok := err.(*Error)
			ass.True(ok)
			ass.NotNil(v)

			Debug = true
			ass.Panics(func() {
				_ = fsm.Event(testE1)
			})
		},
	}
	s2 := &State{
		Index: testS2,
	}
	t1 := &Transition{
		Event: testE1,
		Map:   TransMap{testS1: testS2},
	}

	fsm = New().InitState(s1).AddStates(s2).AddTransitions(t1).MustBuild()
	_ = fsm.Event(testE1)
}
