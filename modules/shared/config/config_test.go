package config

import (
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/tools/clientcmd"
)

var cfg1 = `
dashboard:
  environment: cluster
  session:
    cookie:
      same-site: strict
`

func TestConfig(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "config-test-*.yaml")
	assert.Nil(t, err)
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.WriteString(cfg1)
	assert.Nil(t, err)
	tmpFile.Close()

	cfg, err := NewConfig(viper.New(), tmpFile.Name())
	require.Nil(t, err)
	assert.Equal(t, EnvironmentCluster, cfg.Dashboard.Environment)
	assert.Equal(t, http.SameSiteStrictMode, cfg.Dashboard.Session.Cookie.SameSite)
}

func TestNewConfig(t *testing.T) {
	type args struct {
		v *viper.Viper
		f string
	}
	tests := []struct {
		name    string
		args    args
		want    *Config
		wantErr bool
	}{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewConfig(tt.args.v, tt.args.f)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NewConfig() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestKubeConfigPrecedence tests the precedence of kubeconfig files
// based on the KUBECONFIG environment variable and the kubeconfig flag.
func TestKubeConfigPath(t *testing.T) {

	homedir, err := os.UserHomeDir()
	require.NoError(t, err)

	type args struct {
		v       func() *viper.Viper
		envVars map[string]string
		f       string
	}
	tests := []struct {
		name    string
		args    args
		want    string
		wantErr bool
	}{
		{
			name: "default - no KUBECONFIG set and kubeconfig flag use default",
			args: args{
				v: func() *viper.Viper {
					v := viper.New()
					v.Set(clientcmd.RecommendedConfigPathFlag, clientcmd.RecommendedHomeFile)

					return v
				},
				f:       "",
				envVars: map[string]string{},
			},
			want:    filepath.Join(homedir, ".kube", "config"),
			wantErr: false,
		},
		{
			name: "KUBECONFIG set and kubeconfig flag use default",
			args: args{
				v: func() *viper.Viper {
					v := viper.New()
					v.Set(clientcmd.RecommendedConfigPathFlag, clientcmd.RecommendedHomeFile)

					return v
				},
				f: "",
				envVars: map[string]string{
					"KUBECONFIG": "/tmp/kubie-configeTtA1B.yaml",
				},
			},
			want:    "/tmp/kubie-configeTtA1B.yaml",
			wantErr: false,
		},
		{
			name: "multiple files in KUBECONFIG set - expect first file",
			args: args{
				v: func() *viper.Viper {
					v := viper.New()
					v.Set(clientcmd.RecommendedConfigPathFlag, clientcmd.RecommendedHomeFile)

					return v
				},
				f: "",
				envVars: map[string]string{
					"KUBECONFIG": "/tmp/kubie-configeTtA1B.yaml:/tmp/kubie-configR2D2.yaml",
				},
			},
			want:    "/tmp/kubie-configeTtA1B.yaml",
			wantErr: false,
		},
		{
			name: "KUBECONFIG set and flag set to non-default file location",
			args: args{
				v: func() *viper.Viper {
					v := viper.New()
					v.Set(clientcmd.RecommendedConfigPathFlag, "/tmp/configeTtA1B.yaml")

					return v
				},
				f: "",
				envVars: map[string]string{
					"KUBECONFIG": "/tmp/config-g9kJ8.yaml",
				},
			},
			want:    "/tmp/configeTtA1B.yaml",
			wantErr: false,
		},
		{
			name: "flag set to non-default file location",
			args: args{
				v: func() *viper.Viper {
					v := viper.New()
					v.Set(clientcmd.RecommendedConfigPathFlag, "/tmp/configeTtA1B.yaml")

					return v
				},
				f:       "",
				envVars: map[string]string{},
			},
			want:    "/tmp/configeTtA1B.yaml",
			wantErr: false,
		},
		{
			name: "KUBECONFIG var contains trailing slash which is not a valid file",
			args: args{
				v: func() *viper.Viper {
					v := viper.New()
					v.Set(clientcmd.RecommendedConfigPathFlag, clientcmd.RecommendedHomeFile)

					return v
				},
				f: "",
				envVars: map[string]string{
					"KUBECONFIG": "/tmp/kubie-configeTtA1B.yaml:/tmp/kubie-configR2D2.yaml:/",
				},
			},
			want:    "/tmp/kubie-configeTtA1B.yaml",
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			// set environment variables
			for k, v := range tt.args.envVars {
				t.Setenv(k, v)
			}

			got, err := NewConfig(tt.args.v(), tt.args.f)

			switch tt.wantErr {
			case true:
				assert.Error(t, err)
			case false:
				assert.NoError(t, err)
				assert.Equal(t, tt.want, got.KubeconfigPath)
			}
		})
	}
}
