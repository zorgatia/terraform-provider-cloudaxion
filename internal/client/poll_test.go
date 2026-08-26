package client

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestPollReachesTarget(t *testing.T) {
	states := []string{"provisioning", "provisioning", "running"}
	i := 0

	p := Poll{
		Fetch: func(ctx context.Context) (string, error) {
			s := states[i]
			i++
			return s, nil
		},
		Target:   []string{"running"},
		Pending:  []string{"provisioning"},
		Interval: time.Millisecond,
	}

	if err := p.Wait(context.Background()); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if i != len(states) {
		t.Errorf("polled %d times, want %d", i, len(states))
	}
}

func TestPollMatchesStateCaseInsensitively(t *testing.T) {
	// Disk status is documented as "Active" while VM status is "running";
	// comparisons must not care.
	p := Poll{
		Fetch:    func(ctx context.Context) (string, error) { return "Active", nil },
		Target:   []string{"active"},
		Interval: time.Millisecond,
	}
	if err := p.Wait(context.Background()); err != nil {
		t.Fatalf("Wait: %v", err)
	}
}

func TestPollFailsFastOnUnexpectedState(t *testing.T) {
	calls := 0
	p := Poll{
		Fetch: func(ctx context.Context) (string, error) {
			calls++
			return "error", nil
		},
		Target:   []string{"running"},
		Pending:  []string{"provisioning"},
		Interval: time.Millisecond,
		Timeout:  5 * time.Second,
	}

	err := p.Wait(context.Background())
	if err == nil {
		t.Fatal("expected an error for an unlisted state")
	}
	if calls != 1 {
		t.Errorf("polled %d times, want 1 — an unexpected state must not be waited out", calls)
	}
	if errors.Is(err, ErrPollTimeout) {
		t.Error("an unexpected state should not be reported as a timeout")
	}
}

func TestPollGoneIsDone(t *testing.T) {
	notFound := &APIError{StatusCode: http.StatusNotFound}

	t.Run("delete treats 404 as success", func(t *testing.T) {
		p := Poll{
			Fetch:      func(ctx context.Context) (string, error) { return "", notFound },
			Target:     []string{"deleted"},
			GoneIsDone: true,
			Interval:   time.Millisecond,
		}
		if err := p.Wait(context.Background()); err != nil {
			t.Fatalf("Wait: %v", err)
		}
	})

	t.Run("create treats 404 as failure", func(t *testing.T) {
		p := Poll{
			Fetch:    func(ctx context.Context) (string, error) { return "", notFound },
			Target:   []string{"running"},
			Interval: time.Millisecond,
		}
		if err := p.Wait(context.Background()); !IsNotFound(err) {
			t.Fatalf("err = %v, want a not-found error", err)
		}
	})
}

func TestPollTimeout(t *testing.T) {
	p := Poll{
		Fetch:    func(ctx context.Context) (string, error) { return "provisioning", nil },
		Target:   []string{"running"},
		Pending:  []string{"provisioning"},
		Interval: time.Millisecond,
		Timeout:  50 * time.Millisecond,
	}

	err := p.Wait(context.Background())
	if !errors.Is(err, ErrPollTimeout) {
		t.Fatalf("err = %v, want ErrPollTimeout", err)
	}
}

func TestPollPropagatesFetchError(t *testing.T) {
	sentinel := errors.New("boom")
	p := Poll{
		Fetch:    func(ctx context.Context) (string, error) { return "", sentinel },
		Target:   []string{"running"},
		Interval: time.Millisecond,
	}
	if err := p.Wait(context.Background()); !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want the fetch error", err)
	}
}

func TestPollHonoursCancelledParentContext(t *testing.T) {
	p := Poll{
		Fetch:    func(ctx context.Context) (string, error) { return "provisioning", nil },
		Target:   []string{"running"},
		Pending:  []string{"provisioning"},
		Interval: time.Millisecond,
		Timeout:  time.Hour,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := p.Wait(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}
