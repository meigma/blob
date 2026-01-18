# SLSA Provenance Policy
#
# This policy validates SLSA provenance attestations attached to blob archives.
# It ensures artifacts were built by trusted builders from authorized repositories.
#
# Input structure (provided by the OPA policy engine):
#   {
#     "manifest": { "reference": "...", "digest": "...", "mediaType": "..." },
#     "attestations": [
#       {
#         "_type": "https://in-toto.io/Statement/v1",
#         "predicateType": "https://slsa.dev/provenance/v1",
#         "subject": [...],
#         "predicate": {
#           "buildDefinition": {
#             "buildType": "...",
#             "externalParameters": {
#               "workflow": { "repository": "...", ... }
#             }
#           },
#           "runDetails": {
#             "builder": { "id": "..." }
#           }
#         }
#       }
#     ]
#   }

package blob.policy

import rego.v1

# Default: deny unless explicitly allowed
default allow := false

# Allowed repository organizations (customize for your org)
# Set to empty to allow any repository
allowed_orgs := {
	"meigma",
}

# Allow if we have a valid SLSA provenance from a GitHub Actions workflow
allow if {
	some att in input.attestations
	is_slsa_provenance(att)
	is_github_actions_builder(att)
	is_allowed_repository(att)
}

# Check if the attestation is SLSA provenance
is_slsa_provenance(att) if {
	att.predicateType == "https://slsa.dev/provenance/v1"
}

is_slsa_provenance(att) if {
	att.predicateType == "https://slsa.dev/provenance/v0.2"
}

# Check if the builder is a GitHub Actions workflow
# GitHub attestations use the workflow path as the builder ID:
# e.g., "https://github.com/org/repo/.github/workflows/name.yml@refs/..."
is_github_actions_builder(att) if {
	builder_id := att.predicate.runDetails.builder.id
	startswith(builder_id, "https://github.com/")
	contains(builder_id, "/.github/workflows/")
}

# Check if the source repository is from an allowed organization
is_allowed_repository(att) if {
	# If no orgs are specified, allow any repository
	count(allowed_orgs) == 0
}

is_allowed_repository(att) if {
	count(allowed_orgs) > 0
	repo := get_repository(att)
	some org in allowed_orgs
	startswith(repo, concat("", ["https://github.com/", org, "/"]))
}

# Extract repository from provenance based on build type
get_repository(att) := repo if {
	# GitHub Actions build type (SLSA v1)
	att.predicate.buildDefinition.buildType == "https://actions.github.io/buildtypes/workflow/v1"
	repo := att.predicate.buildDefinition.externalParameters.workflow.repository
}

get_repository(att) := repo if {
	# Generic GitHub source (fallback)
	repo := att.predicate.buildDefinition.externalParameters.source.uri
}

# Deny rules provide specific error messages

deny contains msg if {
	count(input.attestations) == 0
	msg := "no attestations found"
}

deny contains msg if {
	some att in input.attestations
	is_slsa_provenance(att)
	not is_github_actions_builder(att)
	builder_id := att.predicate.runDetails.builder.id
	msg := sprintf("not a GitHub Actions builder: %s", [builder_id])
}

deny contains msg if {
	count(allowed_orgs) > 0
	some att in input.attestations
	is_slsa_provenance(att)
	is_github_actions_builder(att)
	not is_allowed_repository(att)
	repo := get_repository(att)
	msg := sprintf("repository not in allowed organizations: %s", [repo])
}
