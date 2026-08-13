package app

import "github.com/schambon/mdv/internal/terminal"

// result is one read from the terminal.
type result struct {
	event terminal.Event
	err   error
}

// pump serializes terminal reads. The reader goroutine waits for a request
// before each read, so at most one read is ever outstanding: when the viewer
// stops asking, stdin is free for a child process such as an editor.
type pump struct {
	requests chan struct{}
	events   chan result
	done     chan struct{}

	// outstanding is owned by the viewer goroutine alone. It keeps a second
	// read from being queued behind the first, which would let the pump steal
	// a keystroke from an editor running under Suspend.
	outstanding bool
}

func newPump(term terminal.Terminal) *pump {
	p := &pump{
		requests: make(chan struct{}, 1),
		events:   make(chan result, 1),
		done:     make(chan struct{}),
	}

	go func() {
		for {
			select {
			case <-p.done:
				return
			case <-p.requests:
			}

			event, err := term.ReadEvent()
			select {
			case p.events <- result{event: event, err: err}:
			case <-p.done:
				return
			}
		}
	}()

	return p
}

// request asks for one event, unless a read is already outstanding.
func (p *pump) request() {
	if p.outstanding {
		return
	}
	p.outstanding = true
	p.requests <- struct{}{} // buffered, and only ever one in flight
}

// received marks the outstanding read as complete.
func (p *pump) received() {
	p.outstanding = false
}

func (p *pump) stop() {
	close(p.done)
}
