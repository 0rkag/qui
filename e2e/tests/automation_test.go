package tests

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/e2e/internal/client"
	"github.com/autobrr/qui/e2e/internal/containers"
)

// setupAutomationTest sets up a test environment with one qBittorrent instance registered.
// Returns the environment, client, and the registered instance ID.
func setupAutomationTest(t *testing.T) (context.Context, *containers.Env, *client.Client, int) {
	t.Helper()

	ctx := context.Background()
	env := containers.Setup(ctx, t, containers.DefaultConfig(), 1)
	t.Cleanup(func() { env.Teardown(ctx) })

	c := env.Client()
	qbit := env.Instances[0]

	// Create a qBittorrent instance in qui
	instanceID := c.CreateInstance(t, client.InstanceConfig{
		Name:     "automation-test-qbt",
		Host:     qbit.URL,
		Username: "admin",
		Password: qbit.Password,
	})
	t.Cleanup(func() { c.DeleteInstance(t, instanceID) })

	// Wait for instance to be ready
	waitForInstance(t, c, instanceID)

	return ctx, env, c, instanceID
}

func TestAutomationCRUD(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test")
	}
	t.Parallel()

	_, _, c, instanceID := setupAutomationTest(t)

	t.Run("create_automation", func(t *testing.T) {
		payload := validAutomationPayload("Test Rule 1")

		automation := c.CreateAutomation(t, instanceID, payload)

		assert.Greater(t, automation.ID, 0)
		assert.Equal(t, "Test Rule 1", automation.Name)
		assert.Equal(t, "*", automation.TrackerPattern)
		assert.True(t, automation.Enabled)
		assert.NotNil(t, automation.Conditions)
		assert.NotNil(t, automation.Conditions.Pause)
		assert.True(t, automation.Conditions.Pause.Enabled)
	})

	t.Run("list_automations", func(t *testing.T) {
		// Create a second rule
		payload := validAutomationPayload("Test Rule 2")
		c.CreateAutomation(t, instanceID, payload)

		automations := c.ListAutomations(t, instanceID)

		assert.GreaterOrEqual(t, len(automations), 2)

		// Find our rules
		var found1, found2 bool
		for _, a := range automations {
			if a.Name == "Test Rule 1" {
				found1 = true
			}
			if a.Name == "Test Rule 2" {
				found2 = true
			}
		}
		assert.True(t, found1, "Test Rule 1 should be in list")
		assert.True(t, found2, "Test Rule 2 should be in list")
	})

	t.Run("update_automation", func(t *testing.T) {
		// Create a rule to update
		payload := validAutomationPayload("Rule to Update")
		created := c.CreateAutomation(t, instanceID, payload)

		// Update it
		enabled := false
		updatePayload := client.AutomationPayload{
			Name:           "Updated Rule Name",
			TrackerPattern: "*",
			Enabled:        &enabled,
			Conditions: &client.ActionConditions{
				Tag: &client.TagAction{
					Enabled: true,
					Tags:    []string{"test-tag"},
					Mode:    "add",
				},
			},
		}

		updated := c.UpdateAutomation(t, instanceID, created.ID, updatePayload)

		assert.Equal(t, created.ID, updated.ID)
		assert.Equal(t, "Updated Rule Name", updated.Name)
		assert.False(t, updated.Enabled)
		assert.NotNil(t, updated.Conditions.Tag)
		assert.True(t, updated.Conditions.Tag.Enabled)
	})

	t.Run("delete_automation", func(t *testing.T) {
		// Create a rule to delete
		payload := validAutomationPayload("Rule to Delete")
		created := c.CreateAutomation(t, instanceID, payload)

		// Delete it
		c.DeleteAutomation(t, instanceID, created.ID)

		// Verify it's gone
		automations := c.ListAutomations(t, instanceID)
		for _, a := range automations {
			assert.NotEqual(t, created.ID, a.ID, "Deleted rule should not appear in list")
		}
	})

	t.Run("reorder_automations", func(t *testing.T) {
		// Create rules with known order
		rule1 := c.CreateAutomation(t, instanceID, validAutomationPayload("Reorder Test A"))
		rule2 := c.CreateAutomation(t, instanceID, validAutomationPayload("Reorder Test B"))
		rule3 := c.CreateAutomation(t, instanceID, validAutomationPayload("Reorder Test C"))

		// Reorder: C, A, B
		c.ReorderAutomations(t, instanceID, []int{rule3.ID, rule1.ID, rule2.ID})

		// Verify order
		automations := c.ListAutomations(t, instanceID)

		// Find our rules and check sort order
		var orders = make(map[int]int)
		for _, a := range automations {
			if a.ID == rule1.ID || a.ID == rule2.ID || a.ID == rule3.ID {
				orders[a.ID] = a.SortOrder
			}
		}

		// C should come first, then A, then B
		assert.Less(t, orders[rule3.ID], orders[rule1.ID], "C should be before A")
		assert.Less(t, orders[rule1.ID], orders[rule2.ID], "A should be before B")
	})
}

func TestAutomationValidation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test")
	}
	t.Parallel()

	_, _, c, instanceID := setupAutomationTest(t)

	t.Run("reject_missing_name", func(t *testing.T) {
		enabled := true
		payload := client.AutomationPayload{
			Name:           "", // Missing name
			TrackerPattern: "*",
			Enabled:        &enabled,
			Conditions: &client.ActionConditions{
				Pause: &client.PauseAction{Enabled: true},
			},
		}

		resp := c.CreateAutomationRaw(t, instanceID, payload)
		defer resp.Body.Close()

		assert.Equal(t, 400, resp.StatusCode)
	})

	t.Run("reject_missing_action", func(t *testing.T) {
		enabled := true
		payload := client.AutomationPayload{
			Name:           "No Action Rule",
			TrackerPattern: "*",
			Enabled:        &enabled,
			Conditions:     &client.ActionConditions{}, // No actions enabled
		}

		resp := c.CreateAutomationRaw(t, instanceID, payload)
		defer resp.Body.Close()

		assert.Equal(t, 400, resp.StatusCode)
	})

	t.Run("reject_missing_tracker", func(t *testing.T) {
		enabled := true
		payload := client.AutomationPayload{
			Name:           "No Tracker Rule",
			TrackerPattern: "", // No tracker pattern
			Enabled:        &enabled,
			Conditions: &client.ActionConditions{
				Pause: &client.PauseAction{Enabled: true},
			},
		}

		resp := c.CreateAutomationRaw(t, instanceID, payload)
		defer resp.Body.Close()

		assert.Equal(t, 400, resp.StatusCode)
	})

	t.Run("reject_invalid_regex", func(t *testing.T) {
		enabled := true
		payload := client.AutomationPayload{
			Name:           "Invalid Regex Rule",
			TrackerPattern: "*",
			Enabled:        &enabled,
			Conditions: &client.ActionConditions{
				Pause: &client.PauseAction{
					Enabled: true,
					Condition: &client.RuleCondition{
						Field:    "NAME",
						Operator: "MATCHES",
						Value:    "(?!invalid)", // Negative lookahead not supported in RE2
					},
				},
			},
		}

		resp := c.CreateAutomationRaw(t, instanceID, payload)
		defer resp.Body.Close()

		assert.Equal(t, 400, resp.StatusCode)
	})

	t.Run("reject_delete_with_other_action", func(t *testing.T) {
		enabled := true
		payload := client.AutomationPayload{
			Name:           "Delete Plus Other",
			TrackerPattern: "*",
			Enabled:        &enabled,
			Conditions: &client.ActionConditions{
				Delete: &client.DeleteAction{
					Enabled: true,
					Mode:    "delete",
					Condition: &client.RuleCondition{
						Field:    "STATE",
						Operator: "EQUALS",
						Value:    "seeding",
					},
				},
				Pause: &client.PauseAction{
					Enabled: true, // Delete cannot be combined with other actions
				},
			},
		}

		resp := c.CreateAutomationRaw(t, instanceID, payload)
		defer resp.Body.Close()

		assert.Equal(t, 400, resp.StatusCode)
	})

	t.Run("reject_delete_without_condition", func(t *testing.T) {
		enabled := true
		payload := client.AutomationPayload{
			Name:           "Delete No Condition",
			TrackerPattern: "*",
			Enabled:        &enabled,
			Conditions: &client.ActionConditions{
				Delete: &client.DeleteAction{
					Enabled:   true,
					Mode:      "delete",
					Condition: nil, // Delete requires a condition
				},
			},
		}

		resp := c.CreateAutomationRaw(t, instanceID, payload)
		defer resp.Body.Close()

		assert.Equal(t, 400, resp.StatusCode)
	})

	t.Run("reject_category_without_name", func(t *testing.T) {
		enabled := true
		payload := client.AutomationPayload{
			Name:           "Category No Name",
			TrackerPattern: "*",
			Enabled:        &enabled,
			Conditions: &client.ActionConditions{
				Category: &client.CategoryAction{
					Enabled:  true,
					Category: "", // Category name required
				},
			},
		}

		resp := c.CreateAutomationRaw(t, instanceID, payload)
		defer resp.Body.Close()

		assert.Equal(t, 400, resp.StatusCode)
	})

	t.Run("reject_interval_too_short", func(t *testing.T) {
		enabled := true
		interval := 30 // Less than 60 seconds minimum
		payload := client.AutomationPayload{
			Name:            "Short Interval",
			TrackerPattern:  "*",
			Enabled:         &enabled,
			IntervalSeconds: &interval,
			Conditions: &client.ActionConditions{
				Pause: &client.PauseAction{Enabled: true},
			},
		}

		resp := c.CreateAutomationRaw(t, instanceID, payload)
		defer resp.Body.Close()

		assert.Equal(t, 400, resp.StatusCode)
	})
}

func TestAutomationRegexValidation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test")
	}
	t.Parallel()

	_, _, c, instanceID := setupAutomationTest(t)

	t.Run("valid_regex", func(t *testing.T) {
		payload := client.AutomationPayload{
			Name:           "Valid Regex",
			TrackerPattern: "*",
			Conditions: &client.ActionConditions{
				Pause: &client.PauseAction{
					Enabled: true,
					Condition: &client.RuleCondition{
						Field:    "NAME",
						Operator: "MATCHES",
						Value:    "^[a-zA-Z0-9]+$", // Valid RE2 regex
					},
				},
			},
		}

		result := c.ValidateRegex(t, instanceID, payload)

		assert.True(t, result.Valid)
		assert.Empty(t, result.Errors)
	})

	t.Run("invalid_regex_lookahead", func(t *testing.T) {
		payload := client.AutomationPayload{
			Name:           "Invalid Regex",
			TrackerPattern: "*",
			Conditions: &client.ActionConditions{
				Pause: &client.PauseAction{
					Enabled: true,
					Condition: &client.RuleCondition{
						Field:    "NAME",
						Operator: "MATCHES",
						Value:    "(?=lookahead)", // Lookahead not supported in RE2
					},
				},
			},
		}

		result := c.ValidateRegex(t, instanceID, payload)

		assert.False(t, result.Valid)
		require.Len(t, result.Errors, 1)
		assert.Contains(t, result.Errors[0].Message, "invalid")
	})

	t.Run("multiple_invalid_patterns", func(t *testing.T) {
		payload := client.AutomationPayload{
			Name:           "Multiple Invalid",
			TrackerPattern: "*",
			Conditions: &client.ActionConditions{
				Tag: &client.TagAction{
					Enabled: true,
					Tags:    []string{"test"},
					Mode:    "add",
					Condition: &client.RuleCondition{
						Operator: "AND", // Group operator
						Conditions: []*client.RuleCondition{
							{
								Field:    "NAME",
								Operator: "MATCHES",
								Value:    "(?!bad1)", // Invalid
							},
							{
								Field:    "NAME",
								Operator: "MATCHES",
								Value:    "(?<!bad2)", // Invalid
							},
						},
					},
				},
			},
		}

		result := c.ValidateRegex(t, instanceID, payload)

		assert.False(t, result.Valid)
		assert.Len(t, result.Errors, 2)
	})
}

func TestAutomationActivity(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test")
	}
	t.Parallel()

	_, _, c, instanceID := setupAutomationTest(t)

	t.Run("list_activity_empty", func(t *testing.T) {
		activities := c.ListAutomationActivity(t, instanceID, 100)

		// Activity list should be empty or contain previous test runs
		assert.NotNil(t, activities)
	})

	t.Run("delete_activity", func(t *testing.T) {
		// Delete activity older than 0 days (all activity)
		deleted := c.DeleteAutomationActivity(t, instanceID, 0)

		// Should succeed even if nothing to delete
		assert.GreaterOrEqual(t, deleted, int64(0))
	})
}

func TestAutomationWithTrackerDomains(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test")
	}
	t.Parallel()

	_, _, c, instanceID := setupAutomationTest(t)

	t.Run("create_with_tracker_domains", func(t *testing.T) {
		enabled := true
		payload := client.AutomationPayload{
			Name:           "Tracker Domains Rule",
			TrackerDomains: []string{"tracker.example.com", "other.example.org"},
			Enabled:        &enabled,
			Conditions: &client.ActionConditions{
				Pause: &client.PauseAction{Enabled: true},
			},
		}

		automation := c.CreateAutomation(t, instanceID, payload)

		assert.Greater(t, automation.ID, 0)
		assert.Equal(t, "Tracker Domains Rule", automation.Name)
		// TrackerDomains should be normalized and stored
		assert.Len(t, automation.TrackerDomains, 2)
	})
}

func TestAutomationActions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test")
	}
	t.Parallel()

	_, _, c, instanceID := setupAutomationTest(t)

	t.Run("speed_limits_action", func(t *testing.T) {
		enabled := true
		upload := int64(1024)
		download := int64(2048)
		payload := client.AutomationPayload{
			Name:           "Speed Limits Rule",
			TrackerPattern: "*",
			Enabled:        &enabled,
			Conditions: &client.ActionConditions{
				SpeedLimits: &client.SpeedLimitAction{
					Enabled:     true,
					UploadKiB:   &upload,
					DownloadKiB: &download,
				},
			},
		}

		automation := c.CreateAutomation(t, instanceID, payload)

		assert.NotNil(t, automation.Conditions.SpeedLimits)
		assert.True(t, automation.Conditions.SpeedLimits.Enabled)
		assert.Equal(t, int64(1024), *automation.Conditions.SpeedLimits.UploadKiB)
		assert.Equal(t, int64(2048), *automation.Conditions.SpeedLimits.DownloadKiB)
	})

	t.Run("share_limits_action", func(t *testing.T) {
		enabled := true
		ratio := 2.0
		seedingTime := int64(60)
		payload := client.AutomationPayload{
			Name:           "Share Limits Rule",
			TrackerPattern: "*",
			Enabled:        &enabled,
			Conditions: &client.ActionConditions{
				ShareLimits: &client.ShareLimitsAction{
					Enabled:            true,
					RatioLimit:         &ratio,
					SeedingTimeMinutes: &seedingTime,
				},
			},
		}

		automation := c.CreateAutomation(t, instanceID, payload)

		assert.NotNil(t, automation.Conditions.ShareLimits)
		assert.True(t, automation.Conditions.ShareLimits.Enabled)
		assert.Equal(t, 2.0, *automation.Conditions.ShareLimits.RatioLimit)
		assert.Equal(t, int64(60), *automation.Conditions.ShareLimits.SeedingTimeMinutes)
	})

	t.Run("tag_action_with_condition", func(t *testing.T) {
		enabled := true
		payload := client.AutomationPayload{
			Name:           "Tag With Condition",
			TrackerPattern: "*",
			Enabled:        &enabled,
			Conditions: &client.ActionConditions{
				Tag: &client.TagAction{
					Enabled: true,
					Tags:    []string{"seeding", "complete"},
					Mode:    "add",
					Condition: &client.RuleCondition{
						Field:    "PROGRESS",
						Operator: "EQUALS",
						Value:    "1",
					},
				},
			},
		}

		automation := c.CreateAutomation(t, instanceID, payload)

		assert.NotNil(t, automation.Conditions.Tag)
		assert.True(t, automation.Conditions.Tag.Enabled)
		assert.Equal(t, []string{"seeding", "complete"}, automation.Conditions.Tag.Tags)
		assert.Equal(t, "add", automation.Conditions.Tag.Mode)
		assert.NotNil(t, automation.Conditions.Tag.Condition)
	})

	t.Run("category_action", func(t *testing.T) {
		enabled := true
		payload := client.AutomationPayload{
			Name:           "Category Rule",
			TrackerPattern: "*",
			Enabled:        &enabled,
			Conditions: &client.ActionConditions{
				Category: &client.CategoryAction{
					Enabled:  true,
					Category: "movies",
				},
			},
		}

		automation := c.CreateAutomation(t, instanceID, payload)

		assert.NotNil(t, automation.Conditions.Category)
		assert.True(t, automation.Conditions.Category.Enabled)
		assert.Equal(t, "movies", automation.Conditions.Category.Category)
	})

	t.Run("delete_action_with_condition", func(t *testing.T) {
		enabled := true
		payload := client.AutomationPayload{
			Name:           "Delete Rule",
			TrackerPattern: "*",
			Enabled:        &enabled,
			Conditions: &client.ActionConditions{
				Delete: &client.DeleteAction{
					Enabled: true,
					Mode:    "deleteWithFiles",
					Condition: &client.RuleCondition{
						Operator: "AND", // Group operator
						Conditions: []*client.RuleCondition{
							{
								Field:    "RATIO",
								Operator: "GREATER_THAN",
								Value:    "2",
							},
							{
								Field:    "SEEDING_TIME",
								Operator: "GREATER_THAN",
								Value:    "86400", // 24 hours in seconds
							},
						},
					},
				},
			},
		}

		automation := c.CreateAutomation(t, instanceID, payload)

		assert.NotNil(t, automation.Conditions.Delete)
		assert.True(t, automation.Conditions.Delete.Enabled)
		assert.Equal(t, "deleteWithFiles", automation.Conditions.Delete.Mode)
		assert.NotNil(t, automation.Conditions.Delete.Condition)
		assert.Equal(t, "AND", automation.Conditions.Delete.Condition.Operator) // Group operator
		assert.Len(t, automation.Conditions.Delete.Condition.Conditions, 2)
	})
}
