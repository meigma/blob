module github.com/meigma/blob/policy/slsa

go 1.26.5

require (
	github.com/meigma/blob v0.0.0
	github.com/opencontainers/go-digest v1.0.0
	github.com/opencontainers/image-spec v1.1.1
	github.com/secure-systems-lab/go-securesystemslib v0.11.1
	github.com/stretchr/testify v1.12.0
)

require (
	github.com/google/flatbuffers v25.12.19+incompatible // indirect
	github.com/klauspost/compress v1.19.1 // indirect
	golang.org/x/crypto v0.55.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	oras.land/oras-go/v2 v2.6.2 // indirect
)

replace github.com/meigma/blob => ../..
