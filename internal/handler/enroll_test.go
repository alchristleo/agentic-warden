package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// mintToken asks the server for an enrollment token as an administrator.
func mintToken(t *testing.T, srv *httptest.Server, user string) string {
	t.Helper()
	resp := post(t, srv, "/v1/enrollment-tokens", `{"user":"`+user+`"}`)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("minting a token: status = %d", resp.StatusCode)
	}
	var body struct{ Token string }
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	return body.Token
}

// enroll enrolls a machine with a token and returns its credential.
func enroll(t *testing.T, srv *httptest.Server, token string) (machineID, credential string) {
	t.Helper()
	resp := postAs(t, srv, "/v1/machines/enroll", `{"token":"`+token+`","name":"laptop","os":"linux"}`, "")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("enrolling: status = %d", resp.StatusCode)
	}
	var body struct {
		MachineID  string `json:"machineId"`
		Credential string `json:"credential"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	return body.MachineID, body.Credential
}

func TestMintingATokenNeedsTheAdminToken(t *testing.T) {
	srv := newServer(t)

	resp := postAs(t, srv, "/v1/enrollment-tokens", `{"user":"alice@acme.com"}`, "")

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestMintingATokenNeedsAUser(t *testing.T) {
	srv := newServer(t)

	resp := post(t, srv, "/v1/enrollment-tokens", `{}`)

	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", resp.StatusCode)
	}
}

func TestMintingATokenCapsTheTTL(t *testing.T) {
	srv := newServer(t)

	resp := post(t, srv, "/v1/enrollment-tokens", `{"user":"alice@acme.com","ttl":"720h"}`)

	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422: a token must not outlive a week", resp.StatusCode)
	}
}

func TestATokenEnrollsOneMachine(t *testing.T) {
	srv := newServer(t)
	token := mintToken(t, srv, "alice@acme.com")

	id, credential := enroll(t, srv, token)
	if id == "" || credential == "" {
		t.Fatal("enrollment returned no machine id or credential")
	}

	resp := postAs(t, srv, "/v1/machines/enroll", `{"token":"`+token+`","name":"other"}`, "")
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("second enrollment status = %d, want 409", resp.StatusCode)
	}
}

func TestAnUnknownTokenIs404(t *testing.T) {
	srv := newServer(t)

	resp := postAs(t, srv, "/v1/machines/enroll", `{"token":"made-up","name":"x"}`, "")

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestEnrollingWithoutATokenIs422(t *testing.T) {
	srv := newServer(t)

	resp := postAs(t, srv, "/v1/machines/enroll", `{"name":"x"}`, "")

	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", resp.StatusCode)
	}
}

func TestMachinesListShowsEnrollmentsWithoutCredentials(t *testing.T) {
	srv := newServer(t)
	enroll(t, srv, mintToken(t, srv, "alice@acme.com"))
	enroll(t, srv, mintToken(t, srv, "bob@acme.com"))

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/machines", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var machines []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&machines); err != nil {
		t.Fatal(err)
	}
	if len(machines) != 2 || machines[0]["user"] != "alice@acme.com" || machines[1]["user"] != "bob@acme.com" {
		t.Errorf("machines = %v", machines)
	}
	for _, m := range machines {
		if _, leaked := m["credentialHash"]; leaked {
			t.Error("listing leaks the credential hash")
		}
	}
}

func TestMachinesListNeedsTheAdminToken(t *testing.T) {
	srv := newServer(t)

	resp := get(t, srv, "/v1/machines", nil)

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestRevokingAMachine(t *testing.T) {
	srv := newServer(t)
	id, _ := enroll(t, srv, mintToken(t, srv, "alice@acme.com"))

	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/v1/machines/"+id, nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("status = %d, want 204", resp.StatusCode)
	}

	again, _ := http.DefaultClient.Do(req)
	again.Body.Close()
	if again.StatusCode != http.StatusNotFound {
		t.Errorf("revoking twice: status = %d, want 404", again.StatusCode)
	}
}
