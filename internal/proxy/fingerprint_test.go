package proxy

import (
	"encoding/json"
	"reflect"
	"testing"

	"commandcode2api/internal/config"
)

func TestFingerprintMatchesOriginalJavaScriptFixture(t *testing.T) {
	// Captured by evaluating the original JavaScript implementation for user_test.
	const fixture = `{"thumbmark":"0e3f1136fc2fd0f9890be9b7ee921c974564a35f7c250709ed2e479e4d77c40c","components":{"machineIdHash":"b0d4be6b9462b85eaf01b5be6ed01a8fda7518f06bc53ca6ade2124920bbe8fe","macHashes":["85e4cfa868d49942290118fb8fc014c1466c28107bc1cfa4f2e2ae7dc4b149d3","ea6f50f8657df923ccfedfcea9fd48acac6ad4c19d84123a6a1d79b9d332421e","7ff3602e10d9b4030fb6bd6ed6c386294a742410f7de958d328da51c9d302bbe","fb27abec64977a82255920cdb8d8e557b530870c490f2a9c85588df63481bb92","6bf824cfdd5ad50bf3008153506a1d25846f652ffd51c6669a57950fdc153990"],"osUserHash":"bebef7087bbf7f2e551951a1cda04313c1286bc1f2a855427ce3b2cb5e89e150","hostnameHash":"666b837f7d6dd064c3109e102e4aa0bacf49a965da0765cb2bc9ab28625f9095","gitEmailHash":"68803b2b6f9908342abda89fce8371e6912fe879b34d075f346b9552b91ab1e7","platform":"win32","arch":"x64","osRelease":"10.0.22631","cpuModel":"AMD Ryzen 5 7600","cpuCount":6,"memGiB":24,"isContainer":false,"timezone":"Australia/Sydney","runtime":"cli","collectorVersion":1}}`
	var want, got M
	if err := json.Unmarshal([]byte(fixture), &want); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	encoded, _ := json.Marshal(generateFingerprint("user_test", cfg))
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("fingerprint migration changed identity: %s", encoded)
	}
	cfg.FingerprintSalt = "new-device"
	if generateFingerprint("user_test", cfg)["thumbmark"] == got["thumbmark"] {
		t.Fatal("salt did not change identity")
	}
	if generateFingerprint("user_other", config.Default())["thumbmark"] == got["thumbmark"] {
		t.Fatal("keys share identity")
	}
}
