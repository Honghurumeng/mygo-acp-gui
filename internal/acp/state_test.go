package acp

import (
	"encoding/json"
	"testing"
)

func TestListSessionsParamsMarshalsExplicitNullCwd(t *testing.T) {
	data, err := json.Marshal(ListSessionsParams{})
	if err != nil {
		t.Fatal(err)
	}
	// Omitted cwd makes pi-acp fall back to the process workspace. The GUI
	// needs an explicit null to import every workspace.
	if string(data) != `{"cwd":null}` {
		t.Fatalf("empty filter = %s, want {\"cwd\":null}", data)
	}

	cwd := "/work"
	data, err = json.Marshal(ListSessionsParams{Cwd: &cwd, Cursor: "50"})
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"cwd":"/work","cursor":"50"}` {
		t.Fatalf("scoped filter = %s", data)
	}
}
