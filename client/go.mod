module github.com/meigma/blob/client

go 1.25.4

require (
	github.com/meigma/blob v0.0.0
	github.com/opencontainers/go-digest v1.0.0
	github.com/opencontainers/image-spec v1.1.1
	github.com/stretchr/testify v1.11.1
	oras.land/oras-go/v2 v2.6.0
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/google/flatbuffers v25.12.19+incompatible // indirect
	github.com/klauspost/compress v1.18.2 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	golang.org/x/sync v0.19.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace github.com/meigma/blob => ../
