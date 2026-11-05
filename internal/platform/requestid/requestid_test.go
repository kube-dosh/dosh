package requestid

import (
	"context"
	"testing"
)

func TestRequestIDContextRoundTrip(t *testing.T) {
	if got := From(context.Background()); got != "" {
		t.Fatalf("From(background) = %q, want empty", got)
	}
	ctx := With(context.Background(), "req-1")
	if got := From(ctx); got != "req-1" {
		t.Fatalf("From(With(ctx, req-1)) = %q", got)
	}
	if got := From(With(ctx, "req-2")); got != "req-2" {
		t.Fatalf("From(nested context) = %q, want req-2", got)
	}
}
