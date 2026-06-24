package protocol

// ManifestResponseBuilder maps the shared sync response into a generated envelope.
type ManifestResponseBuilder[T any, O any] interface {
	EmptySyncResponse() O
	ErrorSyncResponse(nodeID string, full bool, err error) O
	BuildSyncResponse(nodeID string, response Response[T]) O
}
type ManifestResponseBuilderFuncs[T any, O any] struct {
	Empty func() O
	Error func(nodeID string, full bool, err error) O
	Build func(nodeID string, response Response[T]) O
}
type ManifestResponseValues[T any] struct {
	NodeID              string
	ManifestStartedAtMS int64
	Full                bool
	Changes             []T
	Error               string
}
type ManifestResponseMapper[T any, O any] func(ManifestResponseValues[T]) O

func NewManifestResponseBuilder[T any, O any](mapper ManifestResponseMapper[T, O]) ManifestResponseBuilderFuncs[T, O] {
	return ManifestResponseBuilderFuncs[T, O]{
		Empty: func() O { return mapper(ManifestResponseValues[T]{}) },
		Error: func(nodeID string, full bool, err error) O {
			return mapper(ManifestResponseValues[T]{NodeID: nodeID, Full: full, Error: err.Error()})
		},
		Build: func(nodeID string, response Response[T]) O {
			changes := make([]T, 0, len(response.Changes))
			for _, c := range response.Changes {
				changes = append(changes, c.Payload)
			}
			return mapper(ManifestResponseValues[T]{NodeID: nodeID, ManifestStartedAtMS: response.ManifestStartedAtMS, Full: response.Full, Changes: changes})
		},
	}
}
func (b ManifestResponseBuilderFuncs[T, O]) EmptySyncResponse() O { return b.Empty() }
func (b ManifestResponseBuilderFuncs[T, O]) ErrorSyncResponse(nodeID string, full bool, err error) O {
	return b.Error(nodeID, full, err)
}
func (b ManifestResponseBuilderFuncs[T, O]) BuildSyncResponse(nodeID string, response Response[T]) O {
	return b.Build(nodeID, response)
}
