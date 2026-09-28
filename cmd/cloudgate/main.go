package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/herliansyah/cloudgate/pkg/auth"
	"github.com/herliansyah/cloudgate/pkg/config"
	"github.com/herliansyah/cloudgate/pkg/db"
	"github.com/herliansyah/cloudgate/pkg/server"
	"github.com/herliansyah/cloudgate/pkg/storage"
	"github.com/herliansyah/cloudgate/pkg/updater"
	"github.com/herliansyah/cloudgate/web"
)

func main() {
	args := os.Args[1:]

	if len(args) > 0 {
		switch args[0] {
		case "version", "-v", "--version":
			printVersion()
			return
		case "help", "-h", "--help":
			printHelp()
			return
		case "accounts":
			listAccounts()
			return
		case "audit":
			listAudit()
			return
		case "auth":
			handleAuthCLI(args[1:])
			return
		case "update":
			handleUpdateCLI(args[1:])
			return
		case "changelog":
			handleChangelogCLI()
			return
		case "serve", "server":
			runServer(args[1:])
			return
		}
	}

	// Default action without arguments: run server and launch Web UI
	runServer(args)
}

func printVersion() {
	fmt.Printf("%s v%s\n", config.AppName, config.AppVersion)
	fmt.Printf("Author: %s\n", config.AppAuthor)
	fmt.Printf("Repository: %s\n", config.AppRepo)
}

func printHelp() {
	fmt.Printf("%s v%s - Unified Cloud Storage Gateway\n", config.AppName, config.AppVersion)
	fmt.Printf("Author: %s (%s)\n\n", config.AppAuthor, config.AppRepo)
	fmt.Println("Usage:")
	fmt.Println("  cloudgate                  Start server on 0.0.0.0:5210 and launch browser (default)")
	fmt.Println("  cloudgate serve            Start server in current terminal")
	fmt.Println("    Flags for 'serve' or root:")
	fmt.Println("      --host string          Host to bind (default \"0.0.0.0\" for all IPs)")
	fmt.Println("      --port int             Starting port (default 5210, auto-scans if occupied)")
	fmt.Println("  cloudgate accounts         List connected cloud storage accounts")
	fmt.Println("  cloudgate audit            View recent 100 audit events")
	fmt.Println("  cloudgate auth status      Check GatewayAuth protection status")
	fmt.Println("  cloudgate auth setup <pw>  Set initial MasterPassword from terminal")
	fmt.Println("  cloudgate auth reset       Reset MasterPassword and return to setup state")
	fmt.Println("  cloudgate update [-y]      Check for newer version, verify SHA-256, and apply update")
	fmt.Println("  cloudgate changelog        View offline human-readable version changelog")
	fmt.Println("  cloudgate version          Show version and author information")
	fmt.Println("  cloudgate help             Show this help screen")
}

func runServer(rawArgs []string) {
	fs := flag.NewFlagSet("cloudgate", flag.ContinueOnError)
	hostFlag := fs.String("host", "0.0.0.0", "Host IP to bind (0.0.0.0 listens on all interfaces)")
	portFlag := fs.Int("port", config.DefaultPort, "Starting port number")
	_ = fs.Parse(rawArgs)

	bindHost := *hostFlag
	startPort := *portFlag

	configDir, err := config.Dir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error resolving config directory: %v\n", err)
		os.Exit(1)
	}

	// 1. Scan for available port starting at startPort
	listener, port, err := server.FindAvailableListener(bindHost, startPort, startPort+100)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error binding port: %v\n", err)
		os.Exit(1)
	}

	// 2. Check and acquire Single-Instance process lock
	lockFile, existing, err := config.AcquireInstanceLock(port)
	if err != nil {
		listener.Close()
		if existing != nil {
			url := fmt.Sprintf("http://127.0.0.1:%d", existing.Port)
			fmt.Printf("Notice: %s is already running on this machine (PID: %d, Port: %d).\n", config.AppName, existing.PID, existing.Port)
			fmt.Printf("Opening existing instance in browser: %s\n", url)
			openBrowser(url)
			os.Exit(0)
		}
		fmt.Fprintf(os.Stderr, "Error acquiring instance lock: %v\n", err)
		os.Exit(1)
	}
	defer config.ReleaseInstanceLock(lockFile)

	// 3. Open SQLite database
	database, err := db.Open(configDir)
	if err != nil {
		listener.Close()
		fmt.Fprintf(os.Stderr, "Error opening database: %v\n", err)
		os.Exit(1)
	}
	defer database.Close()

	// 4. Load embedded web assets
	assets, err := web.GetFS()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to load embedded assets: %v\n", err)
	}

	// 5. Initialize Server & Storage Pools
	srv := server.NewServer(database, assets)

	// Setup default StoragePool aggregating all connected accounts
	savedAccounts, _ := database.GetAccounts()
	var drivers []storage.Driver
	for _, acc := range savedAccounts {
		quota := acc.QuotaTotal
		if quota <= 0 {
			quota = 15 * 1024 * 1024 * 1024
		}
		var d storage.Driver
		if acc.Credentials != "" {
			var credMap map[string]string
			if err := json.Unmarshal([]byte(acc.Credentials), &credMap); err != nil {
				credMap = make(map[string]string)
			}
			creds := storage.RcloneCredentials{
				Provider:     storage.Provider(acc.Provider),
				AccountID:    acc.ID,
				ClientID:     credMap["client_id"],
				ClientSecret: credMap["client_secret"],
				AccessToken:  credMap["access_token"],
				RefreshToken: credMap["refresh_token"],
				UserName:     acc.Name,
				Extra:        credMap,
			}
			hasToken := creds.AccessToken != "" || creds.RefreshToken != ""
			hasExtra := len(credMap) > 0
			switch creds.Provider {
			case storage.ProviderGDrive, storage.Provider("google"):
				if hasToken {
					d = storage.NewGDriveDriver(acc.ID, creds.ClientID, creds.ClientSecret, creds.AccessToken, creds.RefreshToken, acc.Email, acc.Name)
				}
			case storage.ProviderOneDrive, storage.ProviderDropbox, storage.ProviderBox, storage.ProviderPCloud, storage.ProviderYandex, storage.ProviderKoofr, storage.ProviderS3, storage.ProviderWebDAV, storage.ProviderMega, storage.ProviderFilen, storage.ProviderB2, storage.ProviderPikPak, storage.ProviderSFTP, storage.ProviderSMB, storage.ProviderProtonDrive:
				if hasToken || hasExtra {
					d = storage.NewRcloneAdapterWithExtra(string(creds.Provider), acc.ID, creds.ClientID, creds.ClientSecret, creds.AccessToken, creds.RefreshToken, acc.Email, acc.Name, credMap)
				}
			default:
				// Unknown provider with credentials — try generic rclone adapter
				if hasToken || hasExtra {
					d = storage.NewRcloneAdapterWithExtra(acc.Provider, acc.ID, creds.ClientID, creds.ClientSecret, creds.AccessToken, creds.RefreshToken, acc.Email, acc.Name, credMap)
				}
			}
		}
		if d == nil {
			// Synthetic adapter for known rclone providers without credentials (legacy or pre-auth)
			switch storage.Provider(acc.Provider) {
			case storage.ProviderS3, storage.ProviderWebDAV, storage.ProviderMega, storage.ProviderKoofr, storage.ProviderBox, storage.ProviderPCloud, storage.ProviderYandex, storage.ProviderOneDrive, storage.ProviderDropbox, storage.ProviderFilen, storage.ProviderB2, storage.ProviderPikPak, storage.ProviderSFTP, storage.ProviderSMB, storage.ProviderProtonDrive:
				d = storage.NewRcloneAdapter(acc.Provider, acc.ID, "", "", "", "", acc.Email, acc.Name)
			default:
				d = storage.NewMemDriver(acc.ID, acc.Provider, quota)
			}
		}
		srv.RegisterDriver(d)
		drivers = append(drivers, d)
	}
	pool := storage.NewStoragePool("all_pool", "Round-Robin All Drives", drivers)
	srv.RegisterPool(pool)

	localURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	lanIPs := getLocalIPv4s()

	printStartupBanner(bindHost, port, localURL, lanIPs, configDir)

	// Auto launch browser to local URL
	go func() {
		time.Sleep(300 * time.Millisecond)
		openBrowser(localURL)
	}()

	// Graceful shutdown and restart handling
	httpServer := &http.Server{Handler: srv.Handler()}
	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, os.Interrupt, syscall.SIGTERM)

	restartChan := make(chan struct{}, 1)
	srv.SetRestartTrigger(func() {
		select {
		case restartChan <- struct{}{}:
		default:
		}
	})

	go func() {
		if err := httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
		}
	}()

	select {
	case <-stopChan:
		fmt.Println("\nShutting down gracefully...")
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(ctx)
		fmt.Println("Cloudgate stopped.")
	case <-restartChan:
		fmt.Println("\nRestarting Cloudgate for applied update...")
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_ = httpServer.Shutdown(ctx)
		cancel()
		_ = database.Close()
		config.ReleaseInstanceLock(lockFile)
		if err := updater.RestartProcess(); err != nil {
			fmt.Fprintf(os.Stderr, "Restart error: %v\n", err)
			os.Exit(1)
		}
	}
}

func getLocalIPv4s() []string {
	var ips []string
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ips
	}
	for _, addr := range addrs {
		if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() {
			if ipNet.IP.To4() != nil {
				ips = append(ips, ipNet.IP.String())
			}
		}
	}
	return ips
}

func listAccounts() {
	configDir, err := config.Dir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return
	}
	database, err := db.Open(configDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return
	}
	defer database.Close()

	accounts, err := database.GetAccounts()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading accounts: %v\n", err)
		return
	}

	if len(accounts) == 0 {
		fmt.Println("No cloud accounts connected yet. Run 'cloudgate' to launch the web dashboard and connect drives.")
		return
	}

	fmt.Printf("%-25s %-15s %-12s %s\n", "NAME", "PROVIDER", "STATUS", "ID")
	fmt.Println("------------------------------------------------------------------")
	for _, acc := range accounts {
		fmt.Printf("%-25s %-15s %-12s %s\n", acc.Name, acc.Provider, acc.Status, acc.ID)
	}
}

func listAudit() {
	configDir, err := config.Dir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return
	}
	database, err := db.Open(configDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return
	}
	defer database.Close()

	events, err := database.GetRecentAuditEvents()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading audit trail: %v\n", err)
		return
	}

	if len(events) == 0 {
		fmt.Println("Audit trail is empty.")
		return
	}

	fmt.Printf("%-20s %-10s %-10s %-25s %s\n", "TIMESTAMP", "ACTION", "STATUS", "TARGET", "ACCOUNT")
	fmt.Println("--------------------------------------------------------------------------------------")
	for _, ev := range events {
		tStr := ev.CreatedAt.Format("2006-01-02 15:04:05")
		fmt.Printf("%-20s %-10s %-10s %-25s %s\n", tStr, ev.Action, ev.Status, ev.Target, ev.AccountID)
	}
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	}
	if cmd != nil {
		_ = cmd.Start()
	}
}

func handleAuthCLI(subArgs []string) {
	if len(subArgs) == 0 {
		fmt.Println("Usage: cloudgate auth <command>")
		fmt.Println("Commands:")
		fmt.Println("  status             Check if GatewayAuth is enabled")
		fmt.Println("  setup <password>   Set initial MasterPassword from terminal")
		fmt.Println("  reset              Clear MasterPassword and return to setup state")
		return
	}

	configDir, err := config.Dir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error resolving config directory: %v\n", err)
		os.Exit(1)
	}

	database, err := db.Open(configDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening database: %v\n", err)
		os.Exit(1)
	}
	defer database.Close()

	switch subArgs[0] {
	case "status":
		enabled, err := database.HasMasterPassword()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error checking status: %v\n", err)
			os.Exit(1)
		}
		if enabled {
			fmt.Println("GatewayAuth: AKTIF (Protected with MasterPassword)")
		} else {
			fmt.Println("GatewayAuth: SETUP DIPERLUKAN (Jalankan 'cloudgate auth setup <password>' untuk inisialisasi)")
		}
	case "setup":
		hasPassword, err := database.HasMasterPassword()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error checking database: %v\n", err)
			os.Exit(1)
		}
		if hasPassword {
			fmt.Fprintf(os.Stderr, "Error: MasterPassword sudah disetel. Gunakan menu ganti kata sandi di web atau jalankan 'cloudgate auth reset' terlebih dahulu.\n")
			os.Exit(1)
		}

		password := ""
		if len(subArgs) > 1 {
			password = subArgs[1]
		} else {
			fmt.Print("Enter new MasterPassword (min 4 characters): ")
			fmt.Scanln(&password)
		}

		if len(password) < 4 {
			fmt.Fprintf(os.Stderr, "Error: Password must be at least 4 characters.\n")
			os.Exit(1)
		}

		hash, err := auth.HashMasterPassword(password)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error hashing password: %v\n", err)
			os.Exit(1)
		}

		if err := database.SetMasterPassword(hash); err != nil {
			fmt.Fprintf(os.Stderr, "Error saving password: %v\n", err)
			os.Exit(1)
		}

		_ = database.RecordAudit("auth", "gateway", "cli", "MasterPassword initialized via CLI", "success", 0)
		fmt.Println("Success: MasterPassword has been set. GatewayAuth is active.")

	case "reset":
		if err := database.ClearMasterPassword(); err != nil {
			fmt.Fprintf(os.Stderr, "Error resetting MasterPassword: %v\n", err)
			os.Exit(1)
		}
		_ = database.RecordAudit("auth", "gateway", "cli", "MasterPassword reset via CLI", "success", 0)
		fmt.Println("Success: MasterPassword removed. Cloudgate reset to initial setup state (SETUP_REQUIRED).")
	default:
		fmt.Fprintf(os.Stderr, "Unknown auth command: %s. Use 'status', 'setup <password>', or 'reset'.\n", subArgs[0])
		os.Exit(1)
	}
}

func handleChangelogCLI() {
	fmt.Println(updater.GetChangelog())
}

func handleUpdateCLI(subArgs []string) {
	fs := flag.NewFlagSet("update", flag.ExitOnError)
	yesFlag := fs.Bool("y", false, "Automatically accept update without interactive confirmation")
	fs.BoolVar(yesFlag, "yes", false, "Automatically accept update without interactive confirmation")
	_ = fs.Parse(subArgs)

	fmt.Printf("Checking for latest release on GitHub (current version: v%s)...\n", config.AppVersion)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	res, err := updater.CheckUpdate(ctx, true)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to check update: %v\n", err)
		os.Exit(1)
	}

	if !res.UpdateAvailable {
		fmt.Printf("Cloudgate is already up to date (v%s).\n", config.AppVersion)
		return
	}

	fmt.Printf("\nNew version available: %s (current: v%s)\n", res.LatestVersion, config.AppVersion)
	if res.ReleaseURL != "" {
		fmt.Printf("Release URL: %s\n", res.ReleaseURL)
	}
	if res.Changelog != "" {
		fmt.Println("\n--- Release Notes ---")
		fmt.Println(res.Changelog)
		fmt.Println("---------------------")
	}

	if res.DownloadURL == "" {
		fmt.Fprintf(os.Stderr, "\nError: Binary for platform (%s_%s) not found in release assets.\n", runtime.GOOS, runtime.GOARCH)
		os.Exit(1)
	}
	if res.ChecksumURL == "" {
		fmt.Fprintf(os.Stderr, "\nError: File checksums.txt not found in release assets. Update aborted for security.\n")
		os.Exit(1)
	}

	if !*yesFlag {
		fmt.Print("\nInstall update now? [y/N]: ")
		var reply string
		_, _ = fmt.Scanln(&reply)
		reply = strings.ToLower(strings.TrimSpace(reply))
		if reply != "y" && reply != "yes" {
			fmt.Println("Update canceled.")
			return
		}
	}

	fmt.Println("\nDownloading and verifying SHA-256 checksums...")
	applyCtx, applyCancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer applyCancel()

	if err := updater.ApplyUpdate(applyCtx, res.DownloadURL, res.ChecksumURL); err != nil {
		fmt.Fprintf(os.Stderr, "Update failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Update applied successfully!")
	fmt.Println("Restarting Cloudgate...")
	if err := updater.RestartProcess(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to restart automatically: %v\nPlease restart the binary manually.\n", err)
	}
}

// ponytail: Stdlib-only terminal ANSI capability check without external dependencies.
// Respects standard NO_COLOR (https://no-color.org/), TERM=dumb, and verifies whether
// stdout is a character device (TTY).
func isColorSupported() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if os.Getenv("TERM") == "dumb" {
		return false
	}
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// ponytail: ANSI High-Intensity Cyan banner with fixed 2-space padding and 75-column framing,
// avoiding heavy terminal-width measurement syscalls or dynamic layout libraries.
func printStartupBanner(bindHost string, port int, localURL string, lanIPs []string, configDir string) {
	const startupBanner = `
   ██████╗██╗      ██████╗ ██╗   ██╗██████╗  ██████╗  █████╗ ████████╗███████╗
  ██╔════╝██║     ██╔═══██╗██║   ██║██╔══██╗██╔════╝ ██╔══██╗╚══██╔══╝██╔════╝
  ██║     ██║     ██║   ██║██║   ██║██║  ██║██║  ███╗███████║   ██║   █████╗  
  ██║     ██║     ██║   ██║██║   ██║██║  ██║██║   ██║██╔══██║   ██║   ██╔══╝  
  ╚██████╗███████╗╚██████╔╝╚██████╔╝██████╔╝╚██████╔╝██║  ██║   ██║   ███████╗
   ╚═════╝╚══════╝ ╚═════╝  ╚═════╝ ╚═════╝  ╚═════╝ ╚═╝  ╚═╝   ╚═╝   ╚══════╝`

	useColor := isColorSupported()
	cyan := ""
	bold := ""
	reset := ""
	if useColor {
		cyan = "\033[1;36m"
		bold = "\033[1m"
		reset = "\033[0m"
	}

	fmt.Println()
	fmt.Printf("%s%s%s\n", cyan, strings.TrimPrefix(startupBanner, "\n"), reset)
	fmt.Println("===========================================================================")
	fmt.Printf("   %s Unified Cloud Storage Gateway v%s\n", config.AppName, config.AppVersion)
	fmt.Printf("   Author     : %s\n", config.AppAuthor)
	fmt.Printf("   Repository : %s\n", config.AppRepo)
	fmt.Printf("   Config Dir : %s\n", configDir)
	fmt.Println("---------------------------------------------------------------------------")
	fmt.Printf("   Local URL   : %s%s%s\n", bold, localURL, reset)
	if len(lanIPs) > 0 {
		for _, ip := range lanIPs {
			fmt.Printf("   Network URL : http://%s:%d  (Access from other devices / LAN)\n", ip, port)
		}
	} else {
		fmt.Printf("   Network URL : http://%s:%d\n", bindHost, port)
	}
	fmt.Println("===========================================================================")
	fmt.Println("Press Ctrl+C to shut down.")
}



