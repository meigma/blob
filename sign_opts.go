package blob

// SignOption configures a Sign operation.
type SignOption func(*signConfig)

// signConfig holds configuration for the Sign operation.
type signConfig struct {
	// Reserved for future options (e.g., annotations, custom artifact type)
}
