package exporter

import (
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/calls"
	"testing"
)

func TestPBXMetricFramesAndOptionalMalformedValues(t *testing.T) {
	c := New(calls.New())
	c.BeginSweep()
	c.Frame(map[string]string{"ActionID": "metrics-CoreStatus", "Response": "Success", "CoreCurrentCalls": "4"})
	if c.pbx["asterisk_current_calls"][0].Value != 4 {
		t.Fatal("core metric")
	}
	c.Frame(map[string]string{"Event": "EndpointList", "ObjectName": "001", "DeviceState": "Not in use", "Auths": "001"})
	c.Frame(map[string]string{"Event": "EndpointListComplete"})
	if c.pbx["asterisk_pjsip_endpoint_up"][0].Labels["kind"] != "extension" || c.pbx["asterisk_pjsip_endpoint_up"][0].Value != 1 {
		t.Fatal("endpoint classification")
	}
	c.Frame(map[string]string{"Event": "QueueParams", "Queue": "600", "Calls": "2", "Completed": "NaN", "Abandoned": "5"})
	c.Frame(map[string]string{"Event": "QueueMember", "Queue": "600", "Status": "2"})
	c.Frame(map[string]string{"Event": "QueueStatusComplete"})
	if len(c.pbx["asterisk_queue_completed_calls"]) != 0 || c.pbx["asterisk_queue_callers"][0].Value != 2 || c.pbx["asterisk_queue_members"][0].Labels["status"] != "in_use" {
		t.Fatal("queue normalization")
	}
}
