package loopd

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/lightninglabs/aperture/lsat"
	"github.com/lightninglabs/loop/loopdb"
	"github.com/lightningnetwork/lnd/cert"
	"github.com/lightningnetwork/lnd/lncfg"
	"github.com/lightningnetwork/lnd/lnrpc"
	"google.golang.org/grpc/credentials"
)

var (
	// LoopDirBase is the default main directory where loop stores its data.
	LoopDirBase = btcutil.AppDataDir("loop", false)

	// DefaultNetwork is the default bitcoin network loop runs on.
	DefaultNetwork = "mainnet"

	defaultLogLevel    = "info"
	defaultLogDirname  = "logs"
	defaultLogFilename = "loopd.log"

	defaultSqliteDatabaseFileName = "loop_sqlite.db"

	defaultLogDir     = filepath.Join(LoopDirBase, defaultLogDirname)
	defaultConfigFile = filepath.Join(
		LoopDirBase, DefaultNetwork, defaultConfigFilename,
	)

	// defaultSqliteDatabasePath is the default path under which we store
	// the SQLite database file.
	defaultSqliteDatabasePath = filepath.Join(
		LoopDirBase, DefaultNetwork, defaultSqliteDatabaseFileName,
	)

	defaultMaxLogFiles         = 3
	defaultMaxLogFileSize      = 10
	defaultLoopOutMaxParts     = uint32(5)
	defaultTotalPaymentTimeout = time.Minute * 60
	defaultMaxPaymentRetries   = 3

	// DefaultTLSCertFilename is the default file name for the autogenerated
	// TLS certificate.
	DefaultTLSCertFilename = "tls.cert"

	// DefaultTLSKeyFilename is the default file name for the autogenerated
	// TLS key.
	DefaultTLSKeyFilename = "tls.key"

	// DatabaseBackendSqlite is the name of the SQLite database backend.
	DatabaseBackendSqlite = "sqlite"

	// DatabaseBackendPostgres is the name of the Postgres database backend.
	DatabaseBackendPostgres = "postgres"

	defaultSelfSignedOrganization = "loop autogenerated cert"

	// defaultLndMacaroon is the default macaroon file we use if the old,
	// deprecated --lnd.macaroondir config option is used.
	defaultLndMacaroon = "admin.macaroon"

	// DefaultLndMacaroonPath is the default mainnet admin macaroon path of
	// LND.
	DefaultLndMacaroonPath = filepath.Join(
		btcutil.AppDataDir("lnd", false),
		"data", "chain", "bitcoin", DefaultNetwork,
		defaultLndMacaroon,
	)

	// DefaultTLSCertPath is the default full path of the autogenerated TLS
	// certificate.
	DefaultTLSCertPath = filepath.Join(
		LoopDirBase, DefaultNetwork, DefaultTLSCertFilename,
	)

	// DefaultTLSKeyPath is the default full path of the autogenerated TLS
	// key.
	DefaultTLSKeyPath = filepath.Join(
		LoopDirBase, DefaultNetwork, DefaultTLSKeyFilename,
	)

	// DefaultMacaroonFilename is the default file name for the
	// autogenerated loop macaroon.
	DefaultMacaroonFilename = "loop.macaroon"

	// DefaultMacaroonPath is the default full path of the base loop
	// macaroon.
	DefaultMacaroonPath = filepath.Join(
		LoopDirBase, DefaultNetwork, DefaultMacaroonFilename,
	)

	// DefaultAutogenValidity is the default validity of a self-signed
	// certificate in number of days.
	DefaultAutogenValidity = 365 * 24 * time.Hour
)

type lndConfig struct {
	Host string `long:"host" description:"lnd instance rpc address"`

	// MacaroonDir is the directory that contains all the macaroon files
	// required for the remote connection.
	MacaroonDir string `long:"macaroondir" description:"DEPRECATED: Use macaroonpath."`

	// MacaroonPath is the path to the single macaroon that should be used
	// instead of needing to specify the macaroon directory that contains
	// all of lnd's macaroons. The specified macaroon MUST have all
	// permissions that all the subservers use, otherwise permission errors
	// will occur.
	MacaroonPath string `long:"macaroonpath" description:"The full path to the single macaroon to use, either the admin.macaroon or a custom baked one. Cannot be specified at the same time as macaroondir. A custom macaroon must contain ALL permissions required for all subservers to work, otherwise permission errors will occur."`

	TLSPath string `long:"tlspath" description:"Path to lnd tls certificate"`
}

type loopServerConfig struct {
	Host  string `long:"host" description:"Loop server address host:port"`
	Proxy string `long:"proxy" description:"The host:port of a SOCKS proxy through which all connections to the loop server will be established over"`

	NoTLS   bool   `long:"notls" description:"Disable tls for communication to the loop server [testing only]"`
	TLSPath string `long:"tlspath" description:"Path to loop server tls certificate [testing only]"`
}

type viewParameters struct{}

type Config struct {
	ShowVersion bool   `long:"version" description:"Display version information and exit"`
	Network     string `long:"network" description:"network to run on" choice:"regtest" choice:"testnet" choice:"mainnet" choice:"simnet"`
	RPCListen   string `long:"rpclisten" description:"Address to listen on for gRPC clients"`
	RESTListen  string `long:"restlisten" description:"Address to listen on for REST clients"`
	CORSOrigin  string `long:"corsorigin" description:"The value to send in the Access-Control-Allow-Origin header. Header will be omitted if empty."`

	LoopDir    string `long:"loopdir" description:"The directory for all of loop's data. If set, this option overwrites --datadir, --logdir, --tlscertpath, --tlskeypath and --macaroonpath."`
	ConfigFile string `long:"configfile" description:"Path to configuration file."`
	DataDir    string `long:"datadir" description:"Directory for loopdb."`

	DatabaseBackend string                 `long:"databasebackend" description:"The database backend to use for storing all asset related data." choice:"sqlite" choice:"postgres"`
	Sqlite          *loopdb.SqliteConfig   `group:"sqlite" namespace:"sqlite"`
	Postgres        *loopdb.PostgresConfig `group:"postgres" namespace:"postgres"`

	TLSCertPath        string        `long:"tlscertpath" description:"Path to write the TLS certificate for loop's RPC and REST services."`
	TLSKeyPath         string        `long:"tlskeypath" description:"Path to write the TLS private key for loop's RPC and REST services."`
	TLSExtraIPs        []string      `long:"tlsextraip" description:"Adds an extra IP to the generated certificate."`
	TLSExtraDomains    []string      `long:"tlsextradomain" description:"Adds an extra domain to the generated certificate."`
	TLSAutoRefresh     bool          `long:"tlsautorefresh" description:"Re-generate TLS certificate and key if the IPs or domains are changed."`
	TLSDisableAutofill bool          `long:"tlsdisableautofill" description:"Do not include the interface IPs or the system hostname in TLS certificate, use first --tlsextradomain as Common Name instead, if set."`
	TLSValidity        time.Duration `long:"tlsvalidity" description:"Loop's TLS certificate validity period in days. Defaults to 8760h (1 year)"`

	MacaroonPath string `long:"macaroonpath" description:"Path to write the macaroon for loop's RPC and REST services if it doesn't exist."`

	LogDir         string `long:"logdir" description:"Directory to log output."`
	MaxLogFiles    int    `long:"maxlogfiles" description:"Maximum logfiles to keep (0 for no rotation)."`
	MaxLogFileSize int    `long:"maxlogfilesize" description:"Maximum logfile size in MB."`

	DebugLevel  string `long:"debuglevel" description:"Logging level for all subsystems {trace, debug, info, warn, error, critical} -- You may also specify <subsystem>=<level>,<subsystem2>=<level>,... to set the log level for individual subsystems -- Use show to list available subsystems"`
	MaxLSATCost uint32 `long:"maxlsatcost" description:"Maximum cost in satoshis that loopd is going to pay for an LSAT token automatically. Does not include routing fees."`
	MaxLSATFee  uint32 `long:"maxlsatfee" description:"Maximum routing fee in satoshis that we are willing to pay while paying for an LSAT token."`

	LoopOutMaxParts uint32 `long:"loopoutmaxparts" description:"The maximum number of payment parts that may be used for a loop out swap."`

	TotalPaymentTimeout time.Duration `long:"totalpaymenttimeout" description:"The timeout to use for off-chain payments."`
	MaxPaymentRetries   int           `long:"maxpaymentretries" description:"The maximum number of times an off-chain payment may be retried."`

	EnableExperimental bool `long:"experimental" description:"Enable experimental features: reservations"`

	Lnd *lndConfig `group:"lnd" namespace:"lnd"`

	Server *loopServerConfig `group:"server" namespace:"server"`

	View viewParameters `command:"view" alias:"v" description:"View all swaps in the database. This command can only be executed when loopd is not running."`
}

const (
	mainnetServer = "swap.lightning.today:11010"
	testnetServer = "test.swap.lightning.today:11010"
)

// DefaultConfig returns all default values for the Config struct.
func DefaultConfig() Config {
	return Config{
		Network:    DefaultNetwork,
		RPCListen:  "localhost:11010",
		RESTListen: "localhost:8081",
		Server: &loopServerConfig{
			NoTLS: false,
		},
		LoopDir:         LoopDirBase,
		ConfigFile:      defaultConfigFile,
		DataDir:         LoopDirBase,
		DatabaseBackend: DatabaseBackendSqlite,
		Sqlite: &loopdb.SqliteConfig{
			DatabaseFileName: defaultSqliteDatabasePath,
		},
		LogDir:              defaultLogDir,
		MaxLogFiles:         defaultMaxLogFiles,
		MaxLogFileSize:      defaultMaxLogFileSize,
		DebugLevel:          defaultLogLevel,
		TLSCertPath:         DefaultTLSCertPath,
		TLSKeyPath:          DefaultTLSKeyPath,
		TLSValidity:         DefaultAutogenValidity,
		MacaroonPath:        DefaultMacaroonPath,
		MaxLSATCost:         lsat.DefaultMaxCostSats,
		MaxLSATFee:          lsat.DefaultMaxRoutingFeeSats,
		LoopOutMaxParts:     defaultLoopOutMaxParts,
		TotalPaymentTimeout: defaultTotalPaymentTimeout,
		MaxPaymentRetries:   defaultMaxPaymentRetries,
		EnableExperimental:  false,
		Lnd: &lndConfig{
			Host:         "localhost:10009",
			MacaroonPath: DefaultLndMacaroonPath,
		},
	}
}

// Validate cleans up paths in the config provided and validates it.
func Validate(cfg *Config) error {
	// Cleanup any paths before we use them.
	cfg.LoopDir = lncfg.CleanAndExpandPath(cfg.LoopDir)
	cfg.DataDir = lncfg.CleanAndExpandPath(cfg.DataDir)
	cfg.LogDir = lncfg.CleanAndExpandPath(cfg.LogDir)
	cfg.TLSCertPath = lncfg.CleanAndExpandPath(cfg.TLSCertPath)
	cfg.TLSKeyPath = lncfg.CleanAndExpandPath(cfg.TLSKeyPath)
	cfg.MacaroonPath = lncfg.CleanAndExpandPath(cfg.MacaroonPath)

	// Since our loop directory overrides our log/data dir values, make sure
	// that they are not set when loop dir is set. We hard here rather than
	// overwriting and potentially confusing the user.
	loopDirSet := cfg.LoopDir != LoopDirBase

	if loopDirSet {
		logDirSet := cfg.LogDir != defaultLogDir
		dataDirSet := cfg.DataDir != LoopDirBase
		tlsCertPathSet := cfg.TLSCertPath != DefaultTLSCertPath
		tlsKeyPathSet := cfg.TLSKeyPath != DefaultTLSKeyPath

		if logDirSet {
			return fmt.Errorf("loopdir overwrites logdir, please " +
				"only set one value")
		}

		if dataDirSet {
			return fmt.Errorf("loopdir overwrites datadir, please " +
				"only set one value")
		}

		if tlsCertPathSet {
			return fmt.Errorf("loopdir overwrites tlscertpath, " +
				"please only set one value")
		}

		if tlsKeyPathSet {
			return fmt.Errorf("loopdir overwrites tlskeypath, " +
				"please only set one value")
		}

		// Once we are satisfied that no other config value was set, we
		// replace them with our loop dir.
		cfg.DataDir = cfg.LoopDir
		cfg.LogDir = filepath.Join(cfg.LoopDir, defaultLogDirname)
	}

	// Append the network type to the data and log directory so they are
	// "namespaced" per network.
	cfg.DataDir = filepath.Join(cfg.DataDir, cfg.Network)
	cfg.LogDir = filepath.Join(cfg.LogDir, cfg.Network)

	// We want the TLS and macaroon files to also be in the "namespaced" sub
	// directory. Replace the default values with actual values in case the
	// user specified either loopdir or datadir.
	if cfg.TLSCertPath == DefaultTLSCertPath {
		cfg.TLSCertPath = filepath.Join(
			cfg.DataDir, DefaultTLSCertFilename,
		)
	}
	if cfg.TLSKeyPath == DefaultTLSKeyPath {
		cfg.TLSKeyPath = filepath.Join(
			cfg.DataDir, DefaultTLSKeyFilename,
		)
	}
	if cfg.MacaroonPath == DefaultMacaroonPath {
		cfg.MacaroonPath = filepath.Join(
			cfg.DataDir, DefaultMacaroonFilename,
		)
	}

	// If the user doesn't specify Lnd.MacaroonPath, we'll reassemble it
	// with the passed Network options.
	if cfg.Lnd.MacaroonPath == DefaultLndMacaroonPath {
		cfg.Lnd.MacaroonPath = filepath.Join(
			btcutil.AppDataDir("lnd", false),
			"data", "chain", "bitcoin", cfg.Network,
			defaultLndMacaroon,
		)
	}

	// We'll also update the database file location as well, if it wasn't
	// set.
	if cfg.Sqlite.DatabaseFileName == defaultSqliteDatabasePath {
		cfg.Sqlite.DatabaseFileName = filepath.Join(
			cfg.DataDir, defaultSqliteDatabaseFileName,
		)
	}

	// If either of these directories do not exist, create them.
	if err := os.MkdirAll(cfg.DataDir, os.ModePerm); err != nil {
		return err
	}

	if err := os.MkdirAll(cfg.LogDir, os.ModePerm); err != nil {
		return err
	}

	// Make sure only one of the macaroon options is used.
	switch {
	case cfg.Lnd.MacaroonPath != "" && cfg.Lnd.MacaroonDir != "":
		return fmt.Errorf("use --lnd.macaroonpath only")

	case cfg.Lnd.MacaroonDir != "":
		// With the new version of lndclient we can only specify a
		// single macaroon instead of all of them. If the old
		// macaroondir is used, we use the admin macaroon located in
		// that directory.
		cfg.Lnd.MacaroonPath = path.Join(
			lncfg.CleanAndExpandPath(cfg.Lnd.MacaroonDir),
			defaultLndMacaroon,
		)

	case cfg.Lnd.MacaroonPath != "":
		cfg.Lnd.MacaroonPath = lncfg.CleanAndExpandPath(
			cfg.Lnd.MacaroonPath,
		)

	default:
		return fmt.Errorf("must specify --lnd.macaroonpath")
	}

	// Allow at most 2x the default total payment timeout.
	if cfg.TotalPaymentTimeout > 2*defaultTotalPaymentTimeout {
		return fmt.Errorf("max total payment timeout allowed is at "+
			"most %v", 2*defaultTotalPaymentTimeout)
	}

	// At least one retry.
	if cfg.MaxPaymentRetries < 1 {
		return fmt.Errorf("max payment retries must be at least 1")
	}

	// TLS Validity period to be at least 24 hours
	if cfg.TLSValidity < time.Hour*24 {
		return fmt.Errorf("TLS certificate minimum validity period is 24h")
	}

	return nil
}

// getTLSConfig generates a new self signed certificate or refreshes an existing
// one if necessary, then returns the full TLS configuration for initializing
// a secure server interface.
func getTLSConfig(cfg *Config) (*tls.Config, *credentials.TransportCredentials,
	error) {

	// Let's load our certificate first or create then load if it doesn't
	// yet exist.
	certData, parsedCert, err := loadCertWithCreate(cfg)
	if err != nil {
		return nil, nil, err
	}

	// If the certificate expired or it was outdated, delete it and the TLS
	// key and generate a new pair.
	if time.Now().After(parsedCert.NotAfter) {
		log.Info("TLS certificate is expired or outdated, " +
			"removing old file then generating a new one")

		err := os.Remove(cfg.TLSCertPath)
		if err != nil {
			return nil, nil, err
		}

		err = os.Remove(cfg.TLSKeyPath)
		if err != nil {
			return nil, nil, err
		}

		certData, _, err = loadCertWithCreate(cfg)
		if err != nil {
			return nil, nil, err
		}
	}

	tlsCfg := cert.TLSConfFromCert(certData)
	tlsCfg.NextProtos = []string{"h2"}
	restCreds, err := credentials.NewClientTLSFromFile(
		cfg.TLSCertPath, "",
	)
	if err != nil {
		return nil, nil, err
	}

	return tlsCfg, &restCreds, nil
}

// loadCertWithCreate tries to load the TLS certificate from disk. If the
// specified cert and key files don't exist, the certificate/key pair is created
// first.
func loadCertWithCreate(cfg *Config) (tls.Certificate, *x509.Certificate,
	error) {

	// Ensure we create TLS key and certificate if they don't exist.
	if !lnrpc.FileExists(cfg.TLSCertPath) &&
		!lnrpc.FileExists(cfg.TLSKeyPath) {

		log.Infof("Generating TLS certificates...")
		certBytes, keyBytes, err := cert.GenCertPair(
			defaultSelfSignedOrganization, cfg.TLSExtraIPs,
			cfg.TLSExtraDomains, cfg.TLSDisableAutofill,
			cfg.TLSValidity,
		)
		if err != nil {
			return tls.Certificate{}, nil, err
		}

		err = cert.WriteCertPair(
			cfg.TLSCertPath, cfg.TLSKeyPath, certBytes, keyBytes,
		)
		if err != nil {
			return tls.Certificate{}, nil, err
		}

		log.Infof("Done generating TLS certificates")
	}

	return cert.LoadCert(cfg.TLSCertPath, cfg.TLSKeyPath)
}
