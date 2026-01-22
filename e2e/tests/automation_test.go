package tests

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/autobrr/qui/e2e/internal/client"
	"github.com/autobrr/qui/e2e/internal/containers"
)

// validAutomationPayload is defined in helpers_test.go

func TestAutomationCRUD(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test")
	}
	t.Parallel()

	ctx := context.Background()
	env := containers.Setup(ctx, t, containers.DefaultConfig(), 1)
	t.Cleanup(func() { env.Teardown(ctx) })

	c := env.Client()
	qbit := env.Instances[0]

	instanceID := c.CreateInstance(t, client.InstanceConfig{
		Name:     "automation-crud-test",
		Host:     qbit.URL,
		Username: "admin",
		Password: qbit.Password,
	})
	t.Cleanup(func() { c.DeleteInstance(t, instanceID) })

	waitForInstance(t, c, instanceID)

	t.Run("create_automation", func(t *testing.T) {
		payload := validAutomationPayload("test-rule-1")

		automation := c.CreateAutomation(t, instanceID, payload)
		t.Cleanup(func() { c.DeleteAutomation(t, instanceID, automation.ID) })

		assert.NotZero(t, automation.ID)
		assert.Equal(t, "test-rule-1", automation.Name)
		assert.Equal(t, "*", automation.TrackerPattern)
		assert.True(t, automation.Enabled)
	})

	t.Run("list_automations", func(t *testing.T) {
		// Create two automations
		auto1 := c.CreateAutomation(t, instanceID, validAutomationPayload("list-test-1"))
		t.Cleanup(func() { c.DeleteAutomation(t, instanceID, auto1.ID) })

		auto2 := c.CreateAutomation(t, instanceID, validAutomationPayload("list-test-2"))
		t.Cleanup(func() { c.DeleteAutomation(t, instanceID, auto2.ID) })

		time.Sleep(500 * time.Millisecond)

		// List and verify
		automations := c.ListAutomations(t, instanceID)
		assert.GreaterOrEqual(t, len(automations), 2)

		names := make([]string, len(automations))
		for i, a := range automations {
			names[i] = a.Name
		}
		assert.Contains(t, names, "list-test-1")
		assert.Contains(t, names, "list-test-2")
	})

	t.Run("update_automation", func(t *testing.T) {
		payload := validAutomationPayload("update-test")
		automation := c.CreateAutomation(t, instanceID, payload)
		t.Cleanup(func() { c.DeleteAutomation(t, instanceID, automation.ID) })

		// Update the name
		payload.Name = "update-test-renamed"
		updated := c.UpdateAutomation(t, instanceID, automation.ID, payload)

		assert.Equal(t, automation.ID, updated.ID)
		assert.Equal(t, "update-test-renamed", updated.Name)
	})

	t.Run("delete_automation", func(t *testing.T) {
		payload := validAutomationPayload("delete-test")
		automation := c.CreateAutomation(t, instanceID, payload)

		// Delete it
		c.DeleteAutomation(t, instanceID, automation.ID)

		time.Sleep(500 * time.Millisecond)

		// Verify it's gone
		automations := c.ListAutomations(t, instanceID)
		for _, a := range automations {
			assert.NotEqual(t, automation.ID, a.ID, "deleted automation should not exist")
		}
	})

	t.Run("reorder_automations", func(t *testing.T) {
		// Create three automations
		auto1 := c.CreateAutomation(t, instanceID, validAutomationPayload("reorder-1"))
		t.Cleanup(func() { c.DeleteAutomation(t, instanceID, auto1.ID) })

		auto2 := c.CreateAutomation(t, instanceID, validAutomationPayload("reorder-2"))
		t.Cleanup(func() { c.DeleteAutomation(t, instanceID, auto2.ID) })

		auto3 := c.CreateAutomation(t, instanceID, validAutomationPayload("reorder-3"))
		t.Cleanup(func() { c.DeleteAutomation(t, instanceID, auto3.ID) })

		time.Sleep(500 * time.Millisecond)

		// Reorder: 3, 1, 2
		c.ReorderAutomations(t, instanceID, []int{auto3.ID, auto1.ID, auto2.ID})

		time.Sleep(500 * time.Millisecond)

		// Verify new order
		automations := c.ListAutomations(t, instanceID)
		orderMap := make(map[int]int)
		for _, a := range automations {
			orderMap[a.ID] = a.SortOrder
		}

		assert.Less(t, orderMap[auto3.ID], orderMap[auto1.ID], "auto3 should come before auto1")
		assert.Less(t, orderMap[auto1.ID], orderMap[auto2.ID], "auto1 should come before auto2")
	})
}

func TestAutomationValidation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test")
	}
	t.Parallel()

	ctx := context.Background()
	env := containers.Setup(ctx, t, containers.DefaultConfig(), 1)
	t.Cleanup(func() { env.Teardown(ctx) })

	c := env.Client()
	qbit := env.Instances[0]

	instanceID := c.CreateInstance(t, client.InstanceConfig{
		Name:     "automation-validation-test",
		Host:     qbit.URL,
		Username: "admin",
		Password: qbit.Password,
	})
	t.Cleanup(func() { c.DeleteInstance(t, instanceID) })

	waitForInstance(t, c, instanceID)

	t.Run("reject_missing_name", func(t *testing.T) {
		payload := validAutomationPayload("")
		payload.Name = "" // Empty name

		errMsg := c.CreateAutomationExpectError(t, instanceID, payload, http.StatusBadRequest)
		assert.Contains(t, errMsg, "Name")
	})

	t.Run("reject_missing_action", func(t *testing.T) {
		enabled := true
		payload := client.AutomationPayload{
			Name:           "no-action-test",
			TrackerPattern: "*",
			Enabled:        &enabled,
			Conditions:     map[string]interface{}{}, // No actions
		}

		errMsg := c.CreateAutomationExpectError(t, instanceID, payload, http.StatusBadRequest)
		assert.Contains(t, errMsg, "action")
	})

	t.Run("reject_invalid_regex", func(t *testing.T) {
		enabled := true
		payload := client.AutomationPayload{
			Name:           "invalid-regex-test",
			TrackerPattern: "*",
			Enabled:        &enabled,
			Conditions: map[string]interface{}{
				"pause": map[string]interface{}{
					"enabled": true,
					"condition": map[string]interface{}{
						"field":    "NAME",
						"operator": "MATCHES",
						"value":    "(?<=invalid)", // Lookbehind not supported in RE2
					},
				},
			},
		}

		errMsg := c.CreateAutomationExpectError(t, instanceID, payload, http.StatusBadRequest)
		assert.Contains(t, errMsg, "regex")
	})

	t.Run("validate_regex_endpoint", func(t *testing.T) {
		enabled := true

		// Valid regex
		validPayload := client.AutomationPayload{
			Name:           "valid-regex",
			TrackerPattern: "*",
			Enabled:        &enabled,
			Conditions: map[string]interface{}{
				"pause": map[string]interface{}{
					"enabled": true,
					"condition": map[string]interface{}{
						"field":    "NAME",
						"operator": "MATCHES",
						"value":    ".*test.*",
					},
				},
			},
		}
		result := c.ValidateRegex(t, instanceID, validPayload)
		assert.True(t, result.Valid)
		assert.Empty(t, result.Errors)

		// Invalid regex
		invalidPayload := client.AutomationPayload{
			Name:           "invalid-regex",
			TrackerPattern: "*",
			Enabled:        &enabled,
			Conditions: map[string]interface{}{
				"pause": map[string]interface{}{
					"enabled": true,
					"condition": map[string]interface{}{
						"field":    "NAME",
						"operator": "MATCHES",
						"value":    "[invalid",
					},
				},
			},
		}
		result = c.ValidateRegex(t, instanceID, invalidPayload)
		assert.False(t, result.Valid)
		assert.NotEmpty(t, result.Errors)
	})
}
