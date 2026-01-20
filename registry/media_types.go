package registry

// Media types for blob archives in OCI registries.
const (
	// ArtifactType identifies blob archives as an OCI 1.1 artifact type.
	ArtifactType = "application/vnd.meigma.blob.v1"

	// MediaTypeIndex is the media type for the FlatBuffers index blob.
	MediaTypeIndex = "application/vnd.meigma.blob.index.v1+flatbuffers"

	// MediaTypeData is the media type for the concatenated data blob.
	MediaTypeData = "application/vnd.meigma.blob.data.v1"
)
