package precompiled

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"testing"

	"github.com/tie-core/common/hex"
)

type TestCase struct {
	Name     string
	Input    []byte
	Expected []byte
	Gas      uint64
}

// decodeHex is a common function for decoding a hex string
func decodeHex(t *testing.T, input string) []byte {
	t.Helper()

	inputDecode, decodeErr := hex.DecodeHex(input)
	if decodeErr != nil {
		t.Fatalf("unable to decode hex, %v", decodeErr)
	}

	return inputDecode
}

func ReadTestCase(t *testing.T, path string, f func(t *testing.T, c *TestCase)) {
	t.Helper()

	data, err := ioutil.ReadFile(filepath.Join("./fixtures", path))
	if err != nil {
		t.Fatal(err)
	}

	type testCase struct {
		Name     string
		Input    string
		Expected string
		Gas      uint64
	}

	var cases []*testCase

	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}

	for _, i := range cases {
		inputDecode := decodeHex(t, fmt.Sprintf("0x%s", i.Input))
		expectedDecode := decodeHex(t, fmt.Sprintf("0x%s", i.Expected))

		c := &TestCase{
			Name:     i.Name,
			Gas:      i.Gas,
			Input:    inputDecode,
			Expected: expectedDecode,
		}

		t.Run(i.Name, func(t *testing.T) {
			f(t, c)
		})
	}
}