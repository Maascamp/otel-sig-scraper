package browser

import (
	"context"
	"time"

	"github.com/chromedp/chromedp"
)

const (
	// DefaultTimeout is the default timeout for browser operations.
	DefaultTimeout = 60 * time.Second

	// DefaultNavigationTimeout is the timeout for page navigation.
	DefaultNavigationTimeout = 30 * time.Second
)

// Pool manages chromedp browser context creation and reuse.
type Pool struct {
	headless bool
	timeout  time.Duration
}

// NewPool creates a new browser pool.
// If headless is true, the browser runs without a visible window.
func NewPool(headless bool) *Pool {
	return &Pool{
		headless: headless,
		timeout:  DefaultTimeout,
	}
}

// SetTimeout overrides the default timeout for browser operations.
func (p *Pool) SetTimeout(d time.Duration) {
	p.timeout = d
}

// NewContext creates a new browser context. The caller must call the returned
// cancel function when done to release resources.
func (p *Pool) NewContext(ctx context.Context) (context.Context, context.CancelFunc) {
	var opts []chromedp.ExecAllocatorOption
	opts = append(opts, chromedp.DefaultExecAllocatorOptions[:]...)

	if !p.headless {
		// Remove the headless flag for visible mode.
		opts = append(opts,
			chromedp.Flag("headless", false),
			chromedp.Flag("disable-gpu", false),
		)
	}

	// Common options for stability.
	opts = append(opts,
		chromedp.Flag("no-first-run", true),
		chromedp.Flag("no-default-browser-check", true),
		chromedp.Flag("disable-background-networking", false),
		chromedp.Flag("disable-extensions", true),
		chromedp.Flag("disable-sync", true),
		chromedp.Flag("disable-translate", true),
		chromedp.WindowSize(1280, 900),
	)

	allocCtx, allocCancel := chromedp.NewExecAllocator(ctx, opts...)
	taskCtx, taskCancel := chromedp.NewContext(allocCtx)

	// Apply timeout.
	timeoutCtx, timeoutCancel := context.WithTimeout(taskCtx, p.timeout)

	cancel := func() {
		timeoutCancel()
		taskCancel()
		allocCancel()
	}

	return timeoutCtx, cancel
}

// Cleanup releases any shared resources held by the pool.
// Currently a no-op since contexts are created per-use, but reserved for
// future connection pooling.
func (p *Pool) Cleanup() {
	// No shared resources to clean up currently.
}
