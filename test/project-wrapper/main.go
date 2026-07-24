package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	authorization "github.com/stackitcloud/stackit-sdk-go/services/authorization/v2api"
	resourcemanager "github.com/stackitcloud/stackit-sdk-go/services/resourcemanager/v0api"
	serviceaccount "github.com/stackitcloud/stackit-sdk-go/services/serviceaccount/v2api"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/utils/set"

	"github.com/stackitcloud/cloud-provider-stackit/test/project-wrapper/sdk"
)

/*
# Purpose

This wrapper is built to run in CI to create a STACKIT portal project to run the end-to-end tests in.
The project will be deleted after the tests have run.

# Required environment variables:

STACKIT_REGION: the region of the STACKIT API endpoint, usually `eu01`
STACKIT_SERVICE_ACCOUNT_TOKEN: service account token with permissions to create projects in `PORTAL_FOLDER_ID`
STACKIT_SERVICE_ACCOUNT_EMAIL: the e-mail address of the service account token
BILLING_REFERENCE: a valid billing reference for the created portal project
PROJECT_OWNER: string representing how is responsible for the created account
PORTAL_FOLDER_ID: the folder in the portal overview in which the e2e portal project will be created
*/

const (
	readinessWaitSeconds = 10
)

func main() {
	if err := checkRequiredEnvironmentVariables(); err != nil {
		log.Println(err)
		os.Exit(1)
	}

	if err := run(); err != nil {
		log.Println(err)
		os.Exit(1)
	}
}

func run() error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	var errs error

	stackitClient, err := sdk.NewClient()
	if err != nil {
		return err
	}

	stackitProjectID, err := createPortalProject(ctx, stackitClient)
	if err != nil {
		return errors.Join(errs, err)
	}
	defer func() {
		log.Printf("Deleting portal project %s.\n", stackitProjectID)
		cleanupErr := deletePortalProject(context.Background(), stackitClient, stackitProjectID)
		if cleanupErr != nil {
			errs = errors.Join(errs, cleanupErr)
		}
	}()

	log.Printf("Created project %s. Waiting for it to become ACTIVE.\n", stackitProjectID)
	if err = waitForProjectReadiness(ctx, stackitClient, stackitProjectID); err != nil {
		return errors.Join(errs, err)
	}

	saKeyJSON, err := createServiceAccountAndKey(ctx, stackitProjectID)
	if err != nil {
		return errors.Join(errs, err)
	}

	// gosec G204/G702 warns when cmd input comes from an external source like os.Args
	// in our case this is expected behavior and the risk of malicious behavior
	// is limited in our CI tooling. Therefore, we can ignore it.
	cmd := exec.CommandContext(ctx, os.Args[1], os.Args[2:]...) // #nosec G204 G702
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("STACKIT_SERVICE_ACCOUNT_KEY=%s", saKeyJSON),
		fmt.Sprintf("STACKIT_PROJECT_ID=%s", stackitProjectID),
	)

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	cmd.Cancel = func() error {
		log.Println("ignoring signal as it should already have been sent to all child processes")
		return nil
	}
	cmd.WaitDelay = 5 * time.Minute

	cmderr := cmd.Run()

	if cmderr != nil {
		errs = errors.Join(errs, fmt.Errorf("integration tests failed: %v", cmderr))
	}

	return errs
}

// checkRequiredEnvironmentVariables verifies if the required environment variables for the STACKIT service are set.
func checkRequiredEnvironmentVariables() error {
	requiredVars := []string{
		"STACKIT_REGION",
		"STACKIT_SERVICE_ACCOUNT_KEY",
		"STACKIT_SERVICE_ACCOUNT_EMAIL",
		"BILLING_REFERENCE",
		"PROJECT_OWNER",
		"PORTAL_FOLDER_ID",
	}

	for _, varName := range requiredVars {
		if os.Getenv(varName) == "" {
			return fmt.Errorf("error: environment variable '%s' is not set", varName)
		}
	}

	return nil
}

// createPortalProject creates a new project in the STACKIT portal using the provided client.
// It generates a random suffix for the project name and uses the provided context for any necessary operations.
// Returns a string representing the ID of the newly created project, or an error if the project creation fails.
func createPortalProject(ctx context.Context, client *sdk.Client) (string, error) {
	projectName := fmt.Sprintf("provider-stackit-integration-%s", generateRandomSuffix(10))

	portalProject, err := client.CreateProject(
		ctx,
		os.Getenv("PORTAL_FOLDER_ID"),
		projectName,
		map[string]string{
			"billingReference": os.Getenv("BILLING_REFERENCE"),
			"scope":            "PUBLIC",
			"purpose":          "provider-stackit-integration-tests",
			"owner":            os.Getenv("PROJECT_OWNER"),
		},
		os.Getenv("STACKIT_SERVICE_ACCOUNT_EMAIL"),
	)

	if err != nil {
		return "", err
	}
	if portalProject.ProjectId == "" {
		return "", fmt.Errorf("error: no project ID found in new portal project '%s'", projectName)
	}
	return portalProject.ProjectId, nil
}

func assignRoleToServiceAccount(ctx context.Context, projectID string, email string, roles set.Set[string]) error {
	client, err := authorization.NewAPIClient()
	if err != nil {
		return err
	}

	_, err = client.DefaultAPI.AddMembers(ctx, projectID).AddMembersPayload(authorization.AddMembersPayload{Members: sdk.GetMembersForRoles(email, roles), ResourceType: "project"}).Execute()
	if err != nil {
		return err
	}
	return nil
}

func RetryWithBackoff[T any](ctx context.Context, backoff wait.Backoff, fn func() (T, error)) (T, error) {
	var result T
	var lastErr error

	waitErr := wait.ExponentialBackoffWithContext(ctx, backoff, func(ctx context.Context) (bool, error) {
		val, err := fn()
		if err != nil {
			lastErr = err
			//nolint:nilerr // Returning nil causes a retry; returning err would stop the backoff.
			return false, nil
		}
		result = val
		return true, nil
	})

	if waitErr != nil {
		return result, fmt.Errorf("backoff failed: %w, last operational error: %v", waitErr, lastErr)
	}

	return result, nil
}

func createServiceAccountAndKey(ctx context.Context, projectID string) (string, error) {
	saClient, err := serviceaccount.NewAPIClient()
	if err != nil {
		return "", fmt.Errorf("creating API client: %v", err)
	}

	createAccountPayload := serviceaccount.CreateServiceAccountPayload{
		Name: "ske-intgrtn-tst",
	}
	resp, err := saClient.DefaultAPI.CreateServiceAccount(ctx, projectID).CreateServiceAccountPayload(createAccountPayload).Execute()
	if err != nil {
		return "", fmt.Errorf("error when calling CreateServiceAccount: %v", err)
	}
	mail := resp.Email
	validUntil := time.Now().Add(time.Hour * 3)

	roles := set.New(sdk.ServiceAccountRoles...)
	err = assignRoleToServiceAccount(ctx, projectID, mail, roles)
	if err != nil {
		return "", fmt.Errorf("error when calling AssignRoleToServiceAccount: %v", err)
	}

	var saKey *serviceaccount.CreateServiceAccountKeyResponse
	saKey, err = RetryWithBackoff(ctx, wait.Backoff{
		Duration: 3 * time.Second,
		Factor:   2.0,
		Steps:    5,
	}, func() (*serviceaccount.CreateServiceAccountKeyResponse, error) {
		saKey, err = saClient.DefaultAPI.CreateServiceAccountKey(ctx, projectID, mail).CreateServiceAccountKeyPayload(serviceaccount.CreateServiceAccountKeyPayload{ValidUntil: &validUntil}).Execute()
		return saKey, err
	})
	if err != nil {
		return "", fmt.Errorf("error when calling CreateServiceAccountKey: %v", err)
	}

	saKeyJson, err := json.Marshal(saKey)
	if err != nil {
		return "", fmt.Errorf("error marshaling SA Key to JSON: %v", err)
	}

	return string(saKeyJson), nil
}

// generateRandomSuffix generates and returns a random alphanumeric string of the specified length.
func generateRandomSuffix(length int) string {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		panic(err)
	}
	return hex.EncodeToString(bytes)[:length]
}

// waitForProjectReadiness waits for a specified portal project to reach the ACTIVE lifecycle state.
// The function waits 1 second in between status checks.
// If the project becomes active within 30 retries, the function returns nil.
// If the project does not become active within 30 seconds, the function returns an error indicating a timeout.
func waitForProjectReadiness(ctx context.Context, client *sdk.Client, stackitProjectID string) error {
	for range 30 {
		project, err := client.GetProject(ctx, stackitProjectID)
		if err != nil {
			log.Printf("Error getting project: %v", err)
			log.Printf("Retrying in %v seconds.\n", readinessWaitSeconds)

			select {
			case <-ctx.Done():
				return fmt.Errorf("context canceled while waiting for project '%s' to become active", stackitProjectID)
			case <-time.After(readinessWaitSeconds * time.Second):
				continue
			}
		}

		if project.LifecycleState == resourcemanager.LIFECYCLESTATE_ACTIVE {
			log.Printf("Project '%s' is now active.\n", stackitProjectID)
			return nil
		}

		log.Printf("Project is not ACTIVE yet, retrying in %v seconds.\n", readinessWaitSeconds)
		time.Sleep(readinessWaitSeconds * time.Second)
	}
	return fmt.Errorf("timeout waiting for project '%s' to become active", stackitProjectID)
}

// deletePortalProject deletes the given project from the STACKIT portal using the provided client.
func deletePortalProject(ctx context.Context, client *sdk.Client, portalProjectID string) error {
	if err := client.DeleteProject(ctx, portalProjectID); err != nil {
		return fmt.Errorf("error deleting project: %w", err)
	}
	return nil
}
