package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

func TestTraceContextHandler(t *testing.T) {
	tests := []struct {
		name       string
		ctx        context.Context
		wantFields map[string]string
	}{
		{
			name: "with span context",
			ctx: trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
				TraceID: trace.TraceID{1},
				SpanID:  trace.SpanID{2},
			})),
			wantFields: map[string]string{
				"traceId": "01000000000000000000000000000000",
				"spanId":  "0200000000000000",
			},
		},
		{
			name: "without span context",
			ctx:  context.Background(),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			logger := slog.New(withTraceContext(slog.NewJSONHandler(&output, nil)))
			logger.InfoContext(test.ctx, "test")

			var record map[string]any
			if err := json.Unmarshal(output.Bytes(), &record); err != nil {
				t.Fatal(err)
			}
			for key, want := range test.wantFields {
				if got := record[key]; got != want {
					t.Errorf("%s = %v, want %s", key, got, want)
				}
			}
			for _, key := range []string{"traceId", "spanId"} {
				if _, want := test.wantFields[key]; !want {
					if _, got := record[key]; got {
						t.Errorf("%s unexpectedly present: %v", key, record[key])
					}
				}
			}
		})
	}
}
