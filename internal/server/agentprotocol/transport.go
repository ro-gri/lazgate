package agentprotocol

import transportstore "laz/internal/queue"

func SelectTransportStore(stores []transportstore.Store) transportstore.Store {
	if len(stores) > 0 && stores[0] != nil {
		return stores[0]
	}
	return transportstore.NopStore{}
}
