package ws

// Handler processes incoming WS messages and dispatches to the pipeline.
// It is embedded in ConnManager for simplicity — the handler logic lives in conn.go.
type Handler struct{}

// NewHandler creates a new Handler.
func NewHandler() *Handler {
	return &Handler{}
}
