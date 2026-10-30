package templates

import (
	"regexp"
	"testing"
)

// The network texts are neutral and functional: they never present a network that is not the public one as a feature, nor promise it.
func TestNetworkMessagesAreNeutral(t *testing.T) {
	banned := regexp.MustCompile(`(?i)private network|rede privada|red privada|not available yet|ainda não|aún no|por enquanto|por ahora|for now|em breve|soon`)
	for lang, table := range messages {
		for _, key := range []string{"STACK_NETWORK_MODE_UNAVAILABLE", "fit_private_network"} {
			text, ok := table[key]
			if !ok {
				t.Fatalf("%s: missing %s", lang, key)
			}
			if banned.MatchString(text) {
				t.Errorf("%s %s: %q presents the private network as a feature", lang, key, text)
			}
		}
	}
}
