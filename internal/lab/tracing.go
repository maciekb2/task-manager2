package lab

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/http"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

type traceSeedKey struct{}
type seededIDs struct{}

func (seededIDs) NewIDs(ctx context.Context) (trace.TraceID, trace.SpanID) {
	var tid trace.TraceID
	if seed, ok := ctx.Value(traceSeedKey{}).(string); ok {
		tid, _ = trace.TraceIDFromHex(seed)
	}
	if !tid.IsValid() {
		_, _ = rand.Read(tid[:])
	}
	return tid, seededIDs{}.NewSpanID(ctx, tid)
}
func (seededIDs) NewSpanID(context.Context, trace.TraceID) trace.SpanID {
	var s trace.SpanID
	_, _ = rand.Read(s[:])
	return s
}
func initTracing(ctx context.Context, role string) (func(context.Context) error, error) {
	exporter, e := otlptracehttp.New(ctx, otlptracehttp.WithEndpoint("jaeger:4318"), otlptracehttp.WithInsecure(), otlptracehttp.WithTimeout(3*time.Second))
	if e != nil {
		return nil, e
	}
	tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exporter, sdktrace.WithBatchTimeout(time.Second), sdktrace.WithMaxQueueSize(4096)), sdktrace.WithIDGenerator(seededIDs{}), sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.AlwaysSample())), sdktrace.WithResource(resource.NewSchemaless(attribute.String("service.name", role), attribute.String("service.version", env("LAB_VERSION", "dev")), attribute.String("k8s.namespace.name", Namespace), attribute.String("k8s.pod.name", os.Getenv("HOSTNAME")))))
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	return tp.Shutdown, nil
}
func traceContext(ctx context.Context, seed string) context.Context {
	return context.WithValue(ctx, traceSeedKey{}, seed)
}
func traceHex(ctx context.Context, fallback string) string {
	s := trace.SpanContextFromContext(ctx)
	if s.IsValid() {
		return s.TraceID().String()
	}
	return fallback
}
func (a *App) operation(ctx context.Context, name string) (context.Context, func(int)) {
	started := time.Now()
	ctx, span := otel.Tracer("systems-lab").Start(ctx, name)
	return ctx, func(status int) {
		if status >= 400 {
			span.SetStatus(codes.Error, fmt.Sprintf("HTTP %d", status))
		}
		span.SetAttributes(attribute.Int("http.response.status_code", status))
		span.End()
		a.emit(Event{Trace: traceHex(ctx, ""), From: a.role, To: a.role, Kind: "span", Operation: name, Started: started, Duration: float64(time.Since(started).Microseconds()) / 1000, Status: status, SpanID: span.SpanContext().SpanID().String()})
	}
}
func (a *App) tracedWork(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/work" {
			next.ServeHTTP(w, r)
			return
		}
		parent := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))
		ctx, span := otel.Tracer("systems-lab").Start(parent, a.role+".handle", trace.WithSpanKind(trace.SpanKindServer))
		defer span.End()
		r = r.WithContext(ctx)
		r.Header.Set("X-Lab-Trace", traceHex(ctx, traceID(r)))
		response := &statusResponse{ResponseWriter: w, status: 200}
		next.ServeHTTP(response, r)
		span.SetAttributes(attribute.String("http.request.method", r.Method), attribute.String("url.path", r.URL.Path), attribute.Int("http.response.status_code", response.status))
		if response.status >= 400 {
			span.SetStatus(codes.Error, fmt.Sprintf("HTTP %d", response.status))
		}
	})
}

type statusResponse struct {
	http.ResponseWriter
	status int
}

func (w *statusResponse) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
