package main

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/verify"
)

const approvalWorkflow = "a99c81819d2f5af36815b542e680ac228a54e429"
const workflowIdentity = "https://github.com/ll2Rawll/MCBE2Friends/.github/workflows/attest-hosting.yml@" + approvalWorkflow
const hostingPredicate = "https://github.com/ll2Rawll/MCBE2Friends/attestations/hosting/v2"

// Production Sigstore roots fetched through the signed TUF repository at release time.
//
//go:embed licensing/trusted_root.json
var approvalTrust []byte

func approvalDigest(subject, nonce string) [sha256.Size]byte {
	return sha256.Sum256([]byte(grantMarker + "\n" + subject + "\n" + nonce + "\n"))
}

func checkAttestation(data []byte, digest [sha256.Size]byte) error {
	var proof bundle.Bundle
	if err := json.Unmarshal(data, &proof); err != nil {
		return err
	}
	trust, err := root.NewTrustedRootFromJSON(approvalTrust)
	if err != nil {
		return err
	}
	verifier, err := verify.NewVerifier(trust, verify.WithTransparencyLog(1), verify.WithObserverTimestamps(1))
	if err != nil {
		return err
	}
	identity, err := verify.NewShortCertificateIdentity("https://token.actions.githubusercontent.com", "", workflowIdentity, "")
	if err != nil {
		return err
	}
	identity.BuildSignerDigest = approvalWorkflow
	identity.SourceRepositoryIdentifier = "1466956134"
	identity.SourceRepositoryOwnerIdentifier = "56369844"
	identity.SourceRepositoryRef = "refs/heads/premium-keys"
	identity.RunnerEnvironment = "github-hosted"
	identity.BuildTrigger = "push"
	policy := verify.NewPolicy(verify.WithArtifactDigest("sha256", digest[:]), verify.WithCertificateIdentity(identity))
	result, err := verifier.Verify(&proof, policy)
	if err != nil {
		return err
	}
	st := result.Statement
	if st == nil || st.PredicateType != hostingPredicate || len(st.Subject) != 1 ||
		st.Subject[0].GetName() != "mcbe2friends-bedrock-hosting-v2" || st.Subject[0].GetDigest()["sha256"] != hex.EncodeToString(digest[:]) {
		return errors.New("unexpected hosting attestation subject or purpose")
	}
	return nil
}
