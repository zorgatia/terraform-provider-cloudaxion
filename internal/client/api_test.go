package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"testing"
	"time"
)

// recorder captures what the client actually sent, so tests can assert on the
// wire format rather than on the Go call.
type recorder struct {
	method      string
	path        string
	query       url.Values
	contentType string
	body        string
}

func recordingServer(t *testing.T, rec *recorder, status int, response string) *Client {
	t.Helper()
	return newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		rec.method = r.Method
		rec.path = r.URL.Path
		rec.query = r.URL.Query()
		rec.contentType = r.Header.Get("Content-Type")
		rec.body = string(body)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if response != "" {
			w.Write([]byte(response))
		}
	}), WithLocation("tun1"))
}

func TestCreateVMSendsFormEncodedFields(t *testing.T) {
	var rec recorder
	c := recordingServer(t, &rec, http.StatusOK, `{"uuid":"vm-1","status":"running"}`)

	reserve := false
	vm, err := c.CreateVM(context.Background(), "", CreateVMRequest{
		Name:       "k8s-node-1",
		OSName:     "ubuntu",
		OSVersion:  "24.04",
		DiskGB:     80,
		VCPU:       4,
		RAM:        8192,
		PublicKeys: []string{"ssh-ed25519 AAAA one", "ssh-ed25519 AAAA two"},
		CloudInit:  `{"runcmd":["/firstboot"]}`,

		ReservePublicIP: &reserve,
	})
	if err != nil {
		t.Fatalf("CreateVM: %v", err)
	}
	if vm.UUID != "vm-1" {
		t.Errorf("uuid = %q", vm.UUID)
	}

	if rec.contentType != "application/x-www-form-urlencoded" {
		t.Errorf("Content-Type = %q, want form encoding", rec.contentType)
	}
	if rec.path != "/v1/tun1/user-resource/vm" {
		t.Errorf("path = %q", rec.path)
	}

	form, err := url.ParseQuery(rec.body)
	if err != nil {
		t.Fatalf("parsing form body: %v", err)
	}
	for field, want := range map[string]string{
		"name":              "k8s-node-1",
		"os_name":           "ubuntu",
		"os_version":        "24.04",
		"disks":             "80",
		"vcpu":              "4",
		"ram":               "8192",
		"reserve_public_ip": "false",
		"cloud_init":        `{"runcmd":["/firstboot"]}`,
	} {
		if got := form.Get(field); got != want {
			t.Errorf("%s = %q, want %q", field, got, want)
		}
	}

	// Several SSH keys are sent by repeating the parameter, not by joining.
	if keys := form["public_keys"]; len(keys) != 2 {
		t.Errorf("public_keys = %v, want two repeated values", keys)
	}

	// An optional pointer left nil must not appear at all, so the API applies
	// its own default rather than receiving a zero value.
	if _, present := form["backup"]; present {
		t.Error("backup should be absent when not set")
	}
	if _, present := form["billing_account_id"]; present {
		t.Error("billing_account_id should be absent when not set")
	}
}

func TestGetVMPassesUUIDAsQueryNotPath(t *testing.T) {
	var rec recorder
	c := recordingServer(t, &rec, http.StatusOK, `{"uuid":"vm-1","status":"running"}`)

	if _, err := c.GetVM(context.Background(), "", "vm-1"); err != nil {
		t.Fatalf("GetVM: %v", err)
	}

	if rec.path != "/v1/tun1/user-resource/vm" {
		t.Errorf("path = %q — the uuid must not become a path segment", rec.path)
	}
	if got := rec.query.Get("uuid"); got != "vm-1" {
		t.Errorf("uuid query = %q", got)
	}
}

func TestVMStorageDecodesUnknownReplicaShapes(t *testing.T) {
	tests := []struct {
		name string
		json string
		want int
	}{
		{"empty array", `{"storage":[{"uuid":"d1","replica":[]}]}`, 0},
		{"strings", `{"storage":[{"uuid":"d1","replica":["r1","r2"]}]}`, 2},
		{"objects", `{"storage":[{"uuid":"d1","replica":[{"uuid":"r1"}]}]}`, 1},
		{"null", `{"storage":[{"uuid":"d1","replica":null}]}`, 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var vm VM
			if err := json.Unmarshal([]byte(tc.json), &vm); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if len(vm.Storage) != 1 {
				t.Fatalf("storage entries = %d", len(vm.Storage))
			}
			if got := len(vm.Storage[0].Replica); got != tc.want {
				t.Errorf("replica entries = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestVMBootDisk(t *testing.T) {
	vm := VM{Storage: []VMStorage{
		{UUID: "data", Primary: false},
		{UUID: "boot", Primary: true},
	}}
	if disk := vm.BootDisk(); disk == nil || disk.UUID != "boot" {
		t.Errorf("BootDisk = %v, want the primary disk", disk)
	}
	if disk := (&VM{}).BootDisk(); disk != nil {
		t.Error("BootDisk on a VM with no storage should be nil")
	}
}

func TestCreatePrivateNetworkSendsNameAsQuery(t *testing.T) {
	var rec recorder
	c := recordingServer(t, &rec, http.StatusOK, `{"uuid":"net-1","name":"core","subnet":"10.1.104.0/24"}`)

	net, err := c.CreatePrivateNetwork(context.Background(), "", "core")
	if err != nil {
		t.Fatalf("CreatePrivateNetwork: %v", err)
	}
	if net.Subnet != "10.1.104.0/24" {
		t.Errorf("subnet = %q", net.Subnet)
	}

	if rec.query.Get("name") != "core" {
		t.Errorf("name query = %q — this endpoint takes the name as a query parameter", rec.query.Get("name"))
	}
	if rec.body != "" {
		t.Errorf("body = %q, want empty", rec.body)
	}
}

func TestFloatingIPIsAddressedByAddress(t *testing.T) {
	var rec recorder
	c := recordingServer(t, &rec, http.StatusOK, `{"address":"1.2.3.4"}`)

	if _, err := c.AssignFloatingIP(context.Background(), "", "1.2.3.4", "vm-1", FloatingIPTargetVM); err != nil {
		t.Fatalf("AssignFloatingIP: %v", err)
	}

	if rec.path != "/v1/tun1/network/ip_addresses/1.2.3.4/assign" {
		t.Errorf("path = %q", rec.path)
	}

	var body map[string]string
	if err := json.Unmarshal([]byte(rec.body), &body); err != nil {
		t.Fatalf("parsing body: %v", err)
	}
	if body["assigned_to"] != "vm-1" || body["assigned_to_resource_type"] != "virtual_machine" {
		t.Errorf("body = %v", body)
	}
}

func TestFirewallRulesAreNormalisedBeforeSending(t *testing.T) {
	var rec recorder
	c := recordingServer(t, &rec, http.StatusOK, `{"uuid":"fw-1"}`)

	port := 6443
	_, err := c.CreateFirewall(context.Background(), "", "control-plane", 12, []FirewallRule{
		{
			// A uuid carried over from a read must not be echoed back.
			UUID:             "stale-uuid",
			Protocol:         "tcp",
			Direction:        DirectionInbound,
			PortStart:        &port,
			PortEnd:          &port,
			EndpointSpecType: EndpointSpecIPPrefixes,
			EndpointSpec:     []string{"10.0.0.0/8"},
		},
		{
			// An unset endpoint type defaults to "any", and any stray prefixes
			// are dropped because they would be meaningless.
			Protocol:     "tcp",
			Direction:    DirectionOutbound,
			EndpointSpec: []string{"10.0.0.0/8"},
		},
	})
	if err != nil {
		t.Fatalf("CreateFirewall: %v", err)
	}

	var body struct {
		Rules []FirewallRule `json:"rules"`
	}
	if err := json.Unmarshal([]byte(rec.body), &body); err != nil {
		t.Fatalf("parsing body: %v", err)
	}
	if len(body.Rules) != 2 {
		t.Fatalf("rules = %d", len(body.Rules))
	}
	if body.Rules[0].UUID != "" {
		t.Errorf("rule uuid = %q, want it stripped", body.Rules[0].UUID)
	}
	if body.Rules[1].EndpointSpecType != EndpointSpecAny {
		t.Errorf("endpoint_spec_type = %q, want %q", body.Rules[1].EndpointSpecType, EndpointSpecAny)
	}
	if len(body.Rules[1].EndpointSpec) != 0 {
		t.Errorf("endpoint_spec = %v, want it dropped for type any", body.Rules[1].EndpointSpec)
	}
}

func TestFirewallDecodesUnknownResourcesAssignedShape(t *testing.T) {
	var fw Firewall
	raw := `{"uuid":"fw-1","display_name":"x","resources_assigned":[{"uuid":"vm-1"},{"uuid":"vm-2"}]}`
	if err := json.Unmarshal([]byte(raw), &fw); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(fw.ResourcesAssigned) != 2 {
		t.Errorf("resources_assigned = %v", fw.ResourcesAssigned)
	}
	if fw.DisplayName != "x" {
		t.Errorf("display_name = %q — the custom unmarshaller must not drop other fields", fw.DisplayName)
	}
}

func TestAddForwardingRuleReadsBackTheRuleUUID(t *testing.T) {
	// The create response carries only the ports; the uuid needed for deletion
	// is only visible by re-reading the load balancer.
	calls := 0
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			w.Write([]byte(`{"source_port":8080,"target_port":80}`))
			return
		}
		w.Write([]byte(`{"uuid":"lb-1","forwarding_rules":[
			{"uuid":"rule-1","source_port":8080,"target_port":80},
			{"uuid":"rule-2","source_port":8443,"target_port":443}
		]}`))
	}), WithLocation("tun1"))

	rule, err := c.AddForwardingRule(context.Background(), "", "lb-1", 8080, 80)
	if err != nil {
		t.Fatalf("AddForwardingRule: %v", err)
	}
	if rule.UUID != "rule-1" {
		t.Errorf("rule uuid = %q, want it resolved by the follow-up read", rule.UUID)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want a create followed by a read", calls)
	}
}

func TestCreateLoadBalancerSendsOnlyPortPairs(t *testing.T) {
	var rec recorder
	c := recordingServer(t, &rec, http.StatusOK, `{"uuid":"lb-1"}`)

	_, err := c.CreateLoadBalancer(context.Background(), "", CreateLoadBalancerRequest{
		DisplayName:      "ingress",
		NetworkUUID:      "net-1",
		BillingAccountID: 12,
		Rules: []ForwardingRule{
			// Server-side metadata must not be echoed back on create.
			{UUID: "stale", SourcePort: 443, TargetPort: 30443, Protocol: "TCP", CreatedAt: "2026-01-01"},
		},
	})
	if err != nil {
		t.Fatalf("CreateLoadBalancer: %v", err)
	}

	var body struct {
		Rules []map[string]any `json:"rules"`
	}
	if err := json.Unmarshal([]byte(rec.body), &body); err != nil {
		t.Fatalf("parsing body: %v", err)
	}
	if len(body.Rules) != 1 {
		t.Fatalf("rules = %d", len(body.Rules))
	}
	if len(body.Rules[0]) != 2 {
		t.Errorf("rule fields = %v, want only source_port and target_port", body.Rules[0])
	}
}

func TestDiskCapacityToleratesEitherSizeField(t *testing.T) {
	if got := (&Disk{SizeGB: 40}).Capacity(); got != 40 {
		t.Errorf("Capacity with size_gb = %d", got)
	}
	if got := (&Disk{Size: 30}).Capacity(); got != 30 {
		t.Errorf("Capacity with size = %d", got)
	}
}

func TestCreateBucketUsesPut(t *testing.T) {
	var rec recorder
	c := recordingServer(t, &rec, http.StatusOK, `{}`)

	bucket, err := c.CreateBucket(context.Background(), "tfstate", 12)
	if err != nil {
		t.Fatalf("CreateBucket: %v", err)
	}
	if rec.method != http.MethodPut {
		t.Errorf("method = %s, want PUT", rec.method)
	}
	// The response sample is empty, so the requested name has to be carried
	// through or the caller ends up with an unidentifiable bucket.
	if bucket.Name != "tfstate" {
		t.Errorf("name = %q, want it preserved from the request", bucket.Name)
	}
}

func TestBucketEndpointsAreNotLocationScoped(t *testing.T) {
	var rec recorder
	c := recordingServer(t, &rec, http.StatusOK, `{"name":"tfstate"}`)

	if _, err := c.GetBucket(context.Background(), "tfstate"); err != nil {
		t.Fatalf("GetBucket: %v", err)
	}
	if rec.path != "/v1/storage/bucket" {
		t.Errorf("path = %q — object storage is account-wide, not per location", rec.path)
	}
}

func TestWaitForVMPollsUntilRunning(t *testing.T) {
	statuses := []string{"provisioning", "provisioning", "running"}
	i := 0
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		status := statuses[min(i, len(statuses)-1)]
		i++
		w.Write([]byte(`{"uuid":"vm-1","status":"` + status + `"}`))
	}), WithLocation("tun1"))

	vm, err := c.WaitForVM(context.Background(), "", "vm-1", []string{VMStatusRunning}, 10*time.Second)
	if err != nil {
		t.Fatalf("WaitForVM: %v", err)
	}
	if vm.Status != VMStatusRunning {
		t.Errorf("status = %q", vm.Status)
	}
}

func TestWaitForVMDeletedTreats404AsDone(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"errors":{"Error":"not found"}}`))
	}), WithLocation("tun1"))

	if err := c.WaitForVMDeleted(context.Background(), "", "vm-1", 5*time.Second); err != nil {
		t.Fatalf("WaitForVMDeleted: %v", err)
	}
}

func TestGetFirewallReturnsNotFoundForMissingUUID(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"uuid":"fw-1"}]`))
	}), WithLocation("tun1"))

	_, err := c.GetFirewall(context.Background(), "", "fw-missing")
	if !IsNotFound(err) {
		t.Fatalf("err = %v, want a not-found error so the resource leaves state", err)
	}
}

func TestCreateDiskIsFormEncoded(t *testing.T) {
	// The documentation shows a JSON body here, but the live API never parses
	// one: it rejects the request with "'billing_account_id' is required" even
	// when that field is present. Only form encoding works.
	var rec recorder
	c := recordingServer(t, &rec, http.StatusOK, `{"uuid":"disk-1","status":"Active","size_gb":20}`)

	disk, err := c.CreateDisk(context.Background(), "", CreateDiskRequest{
		DisplayName:      "etcd-data",
		SizeGB:           20,
		BillingAccountID: 12345,
	})
	if err != nil {
		t.Fatalf("CreateDisk: %v", err)
	}
	if disk.UUID != "disk-1" {
		t.Errorf("uuid = %q", disk.UUID)
	}

	if rec.contentType != "application/x-www-form-urlencoded" {
		t.Fatalf("Content-Type = %q — the disk endpoints do not accept JSON", rec.contentType)
	}

	form, err := url.ParseQuery(rec.body)
	if err != nil {
		t.Fatalf("parsing form body: %v", err)
	}
	for field, want := range map[string]string{
		"size_gb":            "20",
		"billing_account_id": "12345",
		"display_name":       "etcd-data",
	} {
		if got := form.Get(field); got != want {
			t.Errorf("%s = %q, want %q", field, got, want)
		}
	}
	// An unset optional source must be absent, not an empty string.
	if _, present := form["source_image_type"]; present {
		t.Error("source_image_type should be absent when not set")
	}
}

func TestUpdateDiskIsFormEncoded(t *testing.T) {
	var rec recorder
	c := recordingServer(t, &rec, http.StatusOK, `{"uuid":"disk-1"}`)

	name := "renamed"
	readOnly := true
	if _, err := c.UpdateDisk(context.Background(), "", "disk-1", UpdateDiskRequest{
		DisplayName:      &name,
		ReadOnlyBootable: &readOnly,
	}); err != nil {
		t.Fatalf("UpdateDisk: %v", err)
	}

	if rec.contentType != "application/x-www-form-urlencoded" {
		t.Fatalf("Content-Type = %q", rec.contentType)
	}
	form, _ := url.ParseQuery(rec.body)
	if form.Get("display_name") != "renamed" || form.Get("read_only_bootable") != "true" {
		t.Errorf("form = %q", rec.body)
	}
	// A nil field must not be sent at all, or it would clear a value the caller
	// never intended to touch.
	if _, present := form["billing_account_id"]; present {
		t.Error("billing_account_id should be absent when nil")
	}
}

func TestFirewallDecodesResourcesAssignedObjects(t *testing.T) {
	// The live shape, which the documentation only ever shows empty.
	raw := `{"uuid":"fw-1","display_name":"fw","resources_assigned":[
		{"resource_type":"vm","resource_uuid":"6c772f89-d00c-4d41-8acd-0202599c4ed3"}
	]}`

	var fw Firewall
	if err := json.Unmarshal([]byte(raw), &fw); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(fw.ResourcesAssigned) != 1 || fw.ResourcesAssigned[0] != "6c772f89-d00c-4d41-8acd-0202599c4ed3" {
		t.Errorf("resources_assigned = %v, want the resource_uuid extracted", fw.ResourcesAssigned)
	}
}

func TestPrivateNetworkVLANIsNullWhenAbsent(t *testing.T) {
	// The live API omits vlan_id entirely; a plain int would report 0, which
	// reads as a real VLAN.
	var net PrivateNetwork
	if err := json.Unmarshal([]byte(`{"uuid":"n1","name":"core","subnet":"10.1.1.0/24"}`), &net); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if net.VLANID != nil {
		t.Errorf("VLANID = %v, want nil when the field is absent", *net.VLANID)
	}

	if err := json.Unmarshal([]byte(`{"uuid":"n1","vlan_id":104}`), &net); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if net.VLANID == nil || *net.VLANID != 104 {
		t.Errorf("VLANID = %v, want 104 when present", net.VLANID)
	}
}

func TestUsernameSuppressedWhenCloudInitDefinesUsers(t *testing.T) {
	// Sending username alongside a cloud-init document that declares its own
	// users leaves the guest with no working login at all. Three otherwise
	// identical VMs proved it against the live API on 2026-08-26.
	tests := []struct {
		name          string
		cloudInit     string
		wantSuppessed bool
	}{
		{
			name:          "no cloud-init at all",
			cloudInit:     "",
			wantSuppessed: false,
		},
		{
			name:          "cloud-init without a users block",
			cloudInit:     "#cloud-config\nruncmd:\n  - [\"echo\", \"hi\"]\n",
			wantSuppessed: false,
		},
		{
			name:          "cloud-init declaring users",
			cloudInit:     "#cloud-config\nusers:\n  - name: ops\n",
			wantSuppessed: true,
		},
		{
			name:          "users not at the top level does not count",
			cloudInit:     "#cloud-config\nwrite_files:\n  - content: |\n      users:\n        - nope\n",
			wantSuppessed: false,
		},
		{
			name:          "JSON cloud-init declaring users",
			cloudInit:     `{"users":[{"name":"ops"}]}`,
			wantSuppessed: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var rec recorder
			c := recordingServer(t, &rec, http.StatusOK, `{"uuid":"vm-1","status":"running"}`)

			_, err := c.CreateVM(context.Background(), "", CreateVMRequest{
				Name:      "node",
				Username:  "ops",
				Password:  "Passw0rdy",
				CloudInit: tc.cloudInit,
			})
			if err != nil {
				t.Fatalf("CreateVM: %v", err)
			}

			form, err := url.ParseQuery(rec.body)
			if err != nil {
				t.Fatalf("parsing form body: %v", err)
			}

			_, sentUsername := form["username"]
			_, sentPassword := form["password"]
			if tc.wantSuppessed && (sentUsername || sentPassword) {
				t.Errorf("username/password were sent alongside a users block — that breaks the guest")
			}
			if !tc.wantSuppessed && !sentUsername {
				t.Errorf("username should still be sent when cloud-init does not define users")
			}
		})
	}
}
