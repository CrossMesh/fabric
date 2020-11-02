package metanet

var (
	backendStorePath = []string{"backend"}
)

type storedActiveEndpoints struct {
	Endpoints []string `json:"eps"`
}
