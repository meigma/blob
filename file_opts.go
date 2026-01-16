package blob

// createBlobConfig holds configuration for CreateBlob.
type createBlobConfig struct {
	indexName  string
	dataName   string
	createOpts []CreateOption
}

func (c *createBlobConfig) getIndexName() string {
	if c.indexName == "" {
		return DefaultIndexName
	}
	return c.indexName
}

func (c *createBlobConfig) getDataName() string {
	if c.dataName == "" {
		return DefaultDataName
	}
	return c.dataName
}

// CreateBlobOption configures CreateBlob.
type CreateBlobOption func(*createBlobConfig)

// CreateBlobWithIndexName sets the index file name (default: "index.blob").
func CreateBlobWithIndexName(name string) CreateBlobOption {
	return func(c *createBlobConfig) {
		c.indexName = name
	}
}

// CreateBlobWithDataName sets the data file name (default: "data.blob").
func CreateBlobWithDataName(name string) CreateBlobOption {
	return func(c *createBlobConfig) {
		c.dataName = name
	}
}

// CreateBlobWithCompression sets the compression algorithm.
func CreateBlobWithCompression(compression Compression) CreateBlobOption {
	return func(c *createBlobConfig) {
		c.createOpts = append(c.createOpts, CreateWithCompression(compression))
	}
}

// CreateBlobWithChangeDetection sets the change detection mode.
func CreateBlobWithChangeDetection(cd ChangeDetection) CreateBlobOption {
	return func(c *createBlobConfig) {
		c.createOpts = append(c.createOpts, CreateWithChangeDetection(cd))
	}
}

// CreateBlobWithSkipCompression adds skip compression predicates.
func CreateBlobWithSkipCompression(fns ...SkipCompressionFunc) CreateBlobOption {
	return func(c *createBlobConfig) {
		c.createOpts = append(c.createOpts, CreateWithSkipCompression(fns...))
	}
}

// CreateBlobWithMaxFiles limits the number of files in the archive.
func CreateBlobWithMaxFiles(n int) CreateBlobOption {
	return func(c *createBlobConfig) {
		c.createOpts = append(c.createOpts, CreateWithMaxFiles(n))
	}
}
