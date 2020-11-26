package entrypoint

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEntrypoint_parseSecrets(t *testing.T) {
	t.Parallel()
	ep := Entrypoint{}
	data := []struct {
		title string
		arr   []interface{}
		exp   []Secret
		isErr bool
	}{
		{
			title: "string secret",
			arr: []interface{}{
				map[string]interface{}{
					"name":   "foo",
					"secret": "password",
				},
			},
			exp: []Secret{
				{
					Name:   "foo",
					Secret: "password",
				},
			},
		},
		{
			title: "json secret",
			arr: []interface{}{
				map[string]interface{}{
					"name":   "foo",
					"secret": true,
				},
			},
			exp: []Secret{
				{
					Name:   "foo",
					Secret: `true`,
				},
			},
		},
	}
	for _, d := range data {
		d := d
		t.Run(d.title, func(t *testing.T) {
			t.Parallel()
			secrets, err := ep.parseSecrets(d.arr)
			if d.isErr {
				require.NotNil(t, err)
				return
			}
			require.Nil(t, err)
			require.Equal(t, d.exp, secrets)
		})
	}
}
