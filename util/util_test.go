package util

import (
	"fmt"
	"os"
	"testing"

	"github.com/sero-cash/go-czero-import/cpt"
)

func TestMain(m *testing.M) {
	cpt.ZeroInit_NoCircuit()
	os.Exit(m.Run())
}

func Test_Util(t *testing.T) {
	bool := IsValidBase58Address("4xbbVNj1QNLDEJzyjo8jZpJgetT3pjYVkaavDmsD6GQrF2Fn9XSbpFpgPjKGxoraMQDPuGTYuNyYQumAKNsmCqZu")
	fmt.Print(bool)
}
