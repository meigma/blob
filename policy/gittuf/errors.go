package gittuf

import "errors"

// Sentinel errors for gittuf policy validation.
var (
	// ErrNoRepository indicates no source repository URL was configured.
	ErrNoRepository = errors.New("gittuf: repository URL required")

	// ErrNoSLSAProvenance indicates no SLSA provenance attestation was found.
	// The gittuf policy requires SLSA provenance to determine which repository
	// and ref to verify.
	ErrNoSLSAProvenance = errors.New("gittuf: no SLSA provenance found")

	// ErrNoGittufPolicy indicates the source repository has no gittuf policy.
	// This means gittuf is not enabled for the repository.
	ErrNoGittufPolicy = errors.New("gittuf: repository has no gittuf policy")

	// ErrNoRefToVerify indicates no git ref was found to verify.
	// This can happen when SLSA provenance lacks a source ref and no
	// override ref was configured.
	ErrNoRefToVerify = errors.New("gittuf: no ref to verify")

	// ErrCloneFailed indicates the repository clone operation failed.
	ErrCloneFailed = errors.New("gittuf: clone failed")

	// ErrVerificationFailed indicates gittuf verification failed.
	// The underlying gittuf error provides details about the failure.
	ErrVerificationFailed = errors.New("gittuf: verification failed")

	// ErrPullRSLFailed indicates refreshing the RSL from remote failed.
	ErrPullRSLFailed = errors.New("gittuf: pull RSL failed")
)
