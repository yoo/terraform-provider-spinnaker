package api

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/mitchellh/mapstructure"
	gate "github.com/spinnaker/spin/cmd/gateclient"
)

func GetApplication(client *gate.GatewayClient, applicationName string, dest interface{}) error {
	app, resp, err := client.ApplicationControllerApi.GetApplicationUsingGET(client.Context, applicationName, map[string]interface{}{})
	if resp != nil {
		if resp.StatusCode == http.StatusNotFound {
			return fmt.Errorf("Application '%s' not found\n", applicationName)
		} else if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("Encountered an error getting application: %s status code: %d\n", err, resp.StatusCode)
		}
	}

	if err != nil {
		return err
	}

	if err := mapstructure.Decode(app, dest); err != nil {
		return err
	}

	return nil
}

func CreateApplication(client *gate.GatewayClient, applicationName string, application interface{}) error {

	createAppTask := map[string]interface{}{
		"job":         []interface{}{map[string]interface{}{"type": "createApplication", "application": application}},
		"application": applicationName,
		"description": fmt.Sprintf("Create Application: %s", applicationName),
	}

	ref, _, err := client.TaskControllerApi.TaskUsingPOST1(client.Context, createAppTask)
	if err != nil {
		return err
	}

	toks := strings.Split(ref["ref"].(string), "/")
	id := toks[len(toks)-1]

	task, resp, err := client.TaskControllerApi.GetTaskUsingGET1(client.Context, id)
	attempts := 0
	for (task == nil || !taskCompleted(task)) && attempts < 5 {
		toks := strings.Split(ref["ref"].(string), "/")
		id := toks[len(toks)-1]

		task, resp, err = client.TaskControllerApi.GetTaskUsingGET1(client.Context, id)
		attempts += 1
		time.Sleep(time.Duration(attempts*attempts) * time.Second)
	}

	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("Encountered an error saving application, status code: %d\n", resp.StatusCode)
	}
	if !taskSucceeded(task) {
		return fmt.Errorf("Encountered an error saving application, task output was: %v\n", task)
	}

	// HACK:
	// When creating an application with group permissions terraform fails to
	// read the application due to a HTTP 403. It looks like the permissions
	// are set without refreshing the cache.
	//
	// It looks like the responses of the gate API are cached.
	// Try accessing the application until the cache timeout is reached
	// and the API allows access to the application.
	// The default redis cache timeout is 30 seconds.
	attempts = 0
	for attempts < 10 {
		_, resp, _ := client.ApplicationControllerApi.GetApplicationUsingGET(client.Context, applicationName, map[string]interface{}{})
		if resp != nil && resp.StatusCode != 403 {
			break
		}

		attempts += 1
		time.Sleep(time.Duration(10) * time.Second)
	}

	return nil
}

func DeleteAppliation(client *gate.GatewayClient, applicationName string) error {
	jobSpec := map[string]interface{}{
		"type": "deleteApplication",
		"application": map[string]interface{}{
			"name": applicationName,
		},
	}

	deleteAppTask := map[string]interface{}{
		"job":         []interface{}{jobSpec},
		"application": applicationName,
		"description": fmt.Sprintf("Delete Application: %s", applicationName),
	}

	_, resp, err := client.TaskControllerApi.TaskUsingPOST1(client.Context, deleteAppTask)

	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Encountered an error deleting application, status code: %d\n", resp.StatusCode)
	}

	return nil
}

func taskCompleted(task map[string]interface{}) bool {
	taskStatus, exists := task["status"]
	if !exists {
		return false
	}

	COMPLETED := [...]string{"SUCCEEDED", "STOPPED", "SKIPPED", "TERMINAL", "FAILED_CONTINUE"}
	for _, status := range COMPLETED {
		if taskStatus == status {
			return true
		}
	}
	return false
}

func taskSucceeded(task map[string]interface{}) bool {
	taskStatus, exists := task["status"]
	if !exists {
		return false
	}

	SUCCESSFUL := [...]string{"SUCCEEDED", "STOPPED", "SKIPPED"}
	for _, status := range SUCCESSFUL {
		if taskStatus == status {
			return true
		}
	}
	return false
}
