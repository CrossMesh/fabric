package metanet

import "github.com/crossmesh/fabric/metanet/backend"

// KeyReservedError indicates a invalid key for reservation reason.
type KeyReservedError struct {
	key string
}

func (e *KeyReservedError) Error() string {
	return "key \"" + e.key + "\" is reserved by framework"
}

type EndpointExistError struct {
	endpoint backend.Endpoint
}

func (e *EndpointExistError) Error() string {
	return "endpoint " + e.endpoint.String() + "already exists"
}
