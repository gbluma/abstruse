package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bleenco/abstruse/pkg/fs"
	"github.com/bleenco/abstruse/pkg/logger"
	"github.com/bleenco/abstruse/pkg/tlsutil"
	"github.com/bleenco/abstruse/server/auth"
	"github.com/bleenco/abstruse/server/config"
	"github.com/bleenco/abstruse/server/db"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"go.uber.org/zap"
)

var (
	// ConfigFile is path to config file.
	ConfigFile string
	// Config is global export of configuration.
	Config *config.Config
	// Log is application logger.
	Log *zap.Logger
)

// InitDefaults initializes default values for command flags.
func InitDefaults(cmd *cobra.Command, cfgFile string) {
	ConfigFile = cfgFile

	viper.BindPFlag("http.addr", cmd.PersistentFlags().Lookup("http-addr"))
	viper.BindPFlag("http.tls", cmd.PersistentFlags().Lookup("http-tls"))
	viper.BindPFlag("tls.cert", cmd.PersistentFlags().Lookup("tls-cert"))
	viper.BindPFlag("tls.key", cmd.PersistentFlags().Lookup("tls-key"))
	viper.BindPFlag("db.driver", cmd.PersistentFlags().Lookup("db-driver"))
	viper.BindPFlag("db.host", cmd.PersistentFlags().Lookup("db-host"))
	viper.BindPFlag("db.port", cmd.PersistentFlags().Lookup("db-port"))
	viper.BindPFlag("db.user", cmd.PersistentFlags().Lookup("db-user"))
	viper.BindPFlag("db.password", cmd.PersistentFlags().Lookup("db-password"))
	viper.BindPFlag("db.name", cmd.PersistentFlags().Lookup("db-name"))
	viper.BindPFlag("db.charset", cmd.PersistentFlags().Lookup("db-charset"))
	viper.BindPFlag("etcd.name", cmd.PersistentFlags().Lookup("etcd-name"))
	viper.BindPFlag("etcd.host", cmd.PersistentFlags().Lookup("etcd-host"))
	viper.BindPFlag("etcd.clientport", cmd.PersistentFlags().Lookup("etcd-clientport"))
	viper.BindPFlag("etcd.peerport", cmd.PersistentFlags().Lookup("etcd-peerport"))
	viper.BindPFlag("etcd.datadir", cmd.PersistentFlags().Lookup("etcd-datadir"))
	viper.BindPFlag("etcd.username", cmd.PersistentFlags().Lookup("etcd-username"))
	viper.BindPFlag("etcd.password", cmd.PersistentFlags().Lookup("etcd-password"))
	viper.BindPFlag("etcd.rootpassword", cmd.PersistentFlags().Lookup("etcd-rootpassword"))
	viper.BindPFlag("auth.jwtsecret", cmd.PersistentFlags().Lookup("auth-jwtsecret"))
	viper.BindPFlag("auth.jwtexpiry", cmd.PersistentFlags().Lookup("auth-jwtexpiry"))
	viper.BindPFlag("auth.jwtrefreshexpiry", cmd.PersistentFlags().Lookup("auth-jwtrefreshexpiry"))
	viper.BindPFlag("log.level", cmd.PersistentFlags().Lookup("log-level"))
	viper.BindPFlag("log.stdout", cmd.PersistentFlags().Lookup("log-stdout"))
	viper.BindPFlag("log.filename", cmd.PersistentFlags().Lookup("log-filename"))
	viper.BindPFlag("log.maxsize", cmd.PersistentFlags().Lookup("log-max-size"))
	viper.BindPFlag("log.maxbackups", cmd.PersistentFlags().Lookup("log-max-backups"))
	viper.BindPFlag("log.maxage", cmd.PersistentFlags().Lookup("log-max-age"))
}

// InitConfig initializes configuration.
func InitConfig() {
	var err error

	if ConfigFile == "" {
		home, err := fs.GetHomeDir()
		if err != nil {
			fatal(err)
		}
		ConfigFile = filepath.Join(home, "abstruse", "abstruse-server.json")
	}
	viper.SetConfigFile(ConfigFile)

	viper.SetConfigType("json")
	viper.SetEnvPrefix("abstruse")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	if !fs.Exists(viper.ConfigFileUsed()) {
		if !fs.Exists(filepath.Dir(ConfigFile)) {
			if err := fs.MakeDir(filepath.Dir(ConfigFile)); err != nil {
				fatal(err)
			}
		}
		if err = viper.SafeWriteConfigAs(viper.ConfigFileUsed()); err != nil {
			fatal(err)
		}

		Log.Sugar().Infof("config file save to %s", viper.ConfigFileUsed())
	}

	if err = viper.ReadInConfig(); err != nil {
		fatal(err)
	}

	if err = viper.Unmarshal(&Config); err != nil {
		fatal(err)
	}

	if !strings.HasPrefix(Config.Etcd.DataDir, "/") {
		Config.Etcd.DataDir = filepath.Join(filepath.Dir(viper.ConfigFileUsed()), Config.Etcd.DataDir)
	}

	if !strings.HasPrefix(Config.Log.Filename, "/") {
		Config.Log.Filename = filepath.Join(filepath.Dir(viper.ConfigFileUsed()), Config.Log.Filename)
	}

	if !strings.HasPrefix(Config.TLS.Cert, "/") {
		Config.TLS.Cert = filepath.Join(filepath.Dir(viper.ConfigFileUsed()), Config.TLS.Cert)
	}

	if !strings.HasPrefix(Config.TLS.Key, "/") {
		Config.TLS.Key = filepath.Join(filepath.Dir(viper.ConfigFileUsed()), Config.TLS.Key)
	}

	Log, err = logger.NewLogger(Config.Log)
	if err != nil {
		fatal(err)
	}
}

// InitTLS initializes and creates certificate with private key if not exists.
func InitTLS() {
	cert, key := Config.TLS.Cert, Config.TLS.Key
	if !strings.HasPrefix(cert, "/") {
		cert = filepath.Join(filepath.Dir(viper.ConfigFileUsed()), cert)
	}
	if !strings.HasPrefix(key, "/") {
		key = filepath.Join(filepath.Dir(viper.ConfigFileUsed()), key)
	}

	if !fs.Exists(cert) || !fs.Exists(key) {
		Log.Sugar().Infof("generating SSL cert %s and key %s", cert, key)
	}

	if err := tlsutil.CheckAndGenerateCert(cert, key); err != nil {
		fatal(err)
	}
}

// InitDB initializes database connection.
func InitDB() {
	db.Connect(Config.Db, Log)
}

// InitAuthentication populates authentication global config variables.
func InitAuthentication() {
	secret := viper.GetString("auth.jwtsecret")
	expiry, refreshExpiry := viper.GetDuration("auth.jwtexpiry"), viper.GetDuration("auth.jwtrefreshexpiry")
	auth.Init(secret, expiry, refreshExpiry)
}

// SaveConfig saves new configuration and reinitializes services.
func SaveConfig(cfg *config.Config) error {
	Config = cfg

	viper.Set("http.addr", Config.HTTP.Addr)
	viper.Set("http.tls", Config.HTTP.TLS)
	viper.Set("tls.cert", Config.TLS.Cert)
	viper.Set("tls.key", Config.TLS.Key)
	viper.Set("db.driver", Config.Db.Driver)
	viper.Set("db.host", Config.Db.Host)
	viper.Set("db.port", Config.Db.Port)
	viper.Set("db.user", Config.Db.User)
	viper.Set("db.password", Config.Db.Password)
	viper.Set("db.name", Config.Db.Name)
	viper.Set("db.charset", Config.Db.Charset)
	viper.Set("etcd.name", Config.Etcd.Name)
	viper.Set("etcd.host", Config.Etcd.Host)
	viper.Set("etcd.clientport", Config.Etcd.ClientPort)
	viper.Set("etcd.peerport", Config.Etcd.PeerPort)
	viper.Set("etcd.datadir", Config.Etcd.DataDir)
	viper.Set("etcd.username", Config.Etcd.Username)
	viper.Set("etcd.password", Config.Etcd.Password)
	viper.Set("etcd.rootpassword", Config.Etcd.RootPassword)
	viper.Set("auth.jwtsecret", Config.Auth.JWTSecret)
	viper.Set("auth.jwtexpiry", Config.Auth.JWTExpiry)
	viper.Set("auth.jwtrefreshexpiry", Config.Auth.JWTRefreshExpiry)
	viper.Set("log.level", Config.Log.Level)
	viper.Set("log.stdout", Config.Log.Stdout)
	viper.Set("log.filename", Config.Log.Filename)
	viper.Set("log.maxsize", Config.Log.MaxSize)
	viper.Set("log.maxbackups", Config.Log.MaxBackups)
	viper.Set("log.maxage", Config.Log.MaxAge)

	InitAuthentication()
	InitDB()

	Log.Sugar().Infof("saving config file to %s", viper.ConfigFileUsed())
	return viper.WriteConfigAs(viper.ConfigFileUsed())
}

func fatal(msg interface{}) {
	fmt.Println(msg)
	os.Exit(1)
}
