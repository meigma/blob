// Package registry provides a high-level client for pushing and pulling
// blob archives to/from OCI registries.
//
// The client uses the oras subpackage for low-level OCI operations and
// adds blob-archive-specific functionality like manifest caching and
// lazy blob access via HTTP range requests.
package registry
