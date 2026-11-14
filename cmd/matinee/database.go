package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/yeixio/matinee/internal/backup"
	"github.com/yeixio/matinee/internal/config"
	"github.com/yeixio/matinee/internal/store"
)

// openedDatabase is the database the server runs on.
type openedDatabase struct {
	db *store.Store
	// dsn is where it is; configured is what the server's settings say,
	// which is an older database when it was moved and the settings
	// weren't changed yet.
	dsn, configured string
	// backups is nil for PostgreSQL, which Matinee doesn't back up itself.
	backups *backup.Manager
}

// openDatabase opens the server's database: the one its settings say, or
// the one that database was moved to (see internal/store/move.go). A move
// an admin asked for in Settings happens first, before anything else uses
// the database; if it fails, the server carries on with the database it
// has and Settings says why.
func openDatabase(ctx context.Context, cfg config.Config, log *slog.Logger) (openedDatabase, error) {
	configured := cfg.Database()
	waitOpts := func(dsn string) store.Options {
		if !store.IsPostgres(dsn) {
			return store.Options{}
		}
		// PostgreSQL may be starting at the same time (both pods replaced
		// at once on Kubernetes), restarting, or down with the machine it
		// runs on. The server waits for it as long as it takes rather than
		// exiting, and answers meanwhile with a page that says so (see
		// serveWhileWaiting). A wrong password or database name still stops
		// it, since waiting won't help.
		return store.Options{Wait: -1, Waiting: func(err error, next time.Duration) {
			log.Warn("waiting for the database", "retry_in", next, "err", err)
		}}
	}
	for range 5 {
		dsn, err := store.Follow(ctx, configured, waitOpts(configured))
		if err != nil {
			return openedDatabase{}, fmt.Errorf("open database: %w", err)
		}
		if dsn != configured {
			log.Warn("the database was moved; using the new one. Change the server's database setting to it (MATINEE_DATABASE_URL or the Helm chart's database.url), then the old database can go",
				"configured", store.Describe(configured), "using", store.Describe(dsn))
		}
		// A SQLite database is backed up before an upgrade changes it, every
		// day, and when an admin asks (internal/backup); a restore an admin
		// asked for takes its place now, before it's opened.
		opts := waitOpts(dsn)
		var backups *backup.Manager
		if !store.IsPostgres(dsn) {
			backups = &backup.Manager{Dir: cfg.BackupDir(), DBPath: dsn, Log: log}
			if _, err := backup.ApplyRestore(ctx, dsn, cfg.BackupDir(), log); err != nil {
				return openedDatabase{}, fmt.Errorf("restore the database: %w", err)
			}
			opts.BeforeMigrate = backups.BeforeMigrate
		}
		db, err := store.OpenWith(dsn, opts)
		if err != nil {
			return openedDatabase{}, fmt.Errorf("open database: %w", err)
		}
		if backups != nil {
			backups.Store = db
		}
		to, err := db.Setting(ctx, store.SettingMovePending)
		if err != nil || to == "" {
			return openedDatabase{db: db, dsn: dsn, configured: configured, backups: backups}, err
		}
		if err := moveAtStart(ctx, db, dsn, to, log); err != nil {
			return openedDatabase{db: db, dsn: dsn, configured: configured, backups: backups}, nil
		}
		// Opened again: Follow now finds the new database.
		db.Close()
	}
	return openedDatabase{}, errors.New("open database: it was moved too many times in a row")
}

// moveAtStart makes the move an admin asked for. Either way, it's no
// longer asked for after this; a failure is kept for Settings to show.
func moveAtStart(ctx context.Context, db *store.Store, from, to string, log *slog.Logger) error {
	db.DeleteSetting(ctx, store.SettingMovePending)
	start := "starting"
	moving.Store(&start)
	defer moving.Store(nil)
	log.Info("moving the database", "from", store.Describe(from), "to", store.Describe(to))
	var last time.Time
	report, err := db.MoveTo(ctx, to, func(table string, done, total int64) {
		p := fmt.Sprintf("Copied %d of %d rows.", done, total)
		moving.Store(&p)
		if time.Since(last) > 5*time.Second {
			last = time.Now()
			log.Info("moving the database", "table", table, "rows", done, "of", total)
		}
	})
	if err == nil {
		err = db.FinishMove(ctx, from, to, report)
	}
	if err != nil {
		log.Error("the database move failed; Matinee carries on with the database it has, which wasn't changed", "to", store.Describe(to), "err", err)
		db.SetSetting(ctx, store.SettingMoveError, err.Error())
		return err
	}
	db.DeleteSetting(ctx, store.SettingMoveError)
	log.Info("moved the database; every table has the same number of rows in both",
		"to", store.Describe(to), "tables", len(report.Tables), "rows", report.Rows, "took", report.Took.Round(time.Millisecond))
	return nil
}

// databaseCommand is `matinee database`: moving the database by hand,
// with the server stopped.
func databaseCommand() error {
	args := os.Args[2:]
	usage := func() error {
		fmt.Fprint(os.Stderr, `Usage:
  matinee database where                Say which database Matinee uses
  matinee database move --to <database> Copy the database to another one and switch over to it
  matinee database undo-move            Use the database from before a move again

<database> is a PostgreSQL URL (postgres://user:password@host:5432/matinee)
or a SQLite file's full path (/data/matinee.db). Stop the Matinee server
before a move. Other flags, such as --data and --database-url, say which
database Matinee uses now, the same as for the server.
`)
		return errors.New("say what to do")
	}
	if len(args) == 0 {
		return usage()
	}
	ctx := context.Background()
	switch args[0] {
	case "where":
		cfg, err := config.Load(args[1:])
		if err != nil {
			return err
		}
		dsn, err := store.Follow(ctx, cfg.Database(), store.Options{})
		if err != nil {
			return err
		}
		fmt.Println("Matinee uses", store.Describe(dsn))
		if dsn != cfg.Database() {
			fmt.Println("It was moved from", store.Describe(cfg.Database())+", which its settings still say. Change the settings to the new database.")
		}
		return nil
	case "move":
		fs := flag.NewFlagSet("move", flag.ContinueOnError)
		to := fs.String("to", "", "the database to move to: a PostgreSQL URL or a SQLite file's full path")
		noSwitch := fs.Bool("copy-only", false, "copy and check, but keep using the old database")
		rest := splitFlags(args[1:], "to", "copy-only")
		if err := fs.Parse(rest.own); err != nil {
			return err
		}
		if *to == "" {
			return usage()
		}
		cfg, err := config.Load(rest.others)
		if err != nil {
			return err
		}
		return moveByHand(ctx, cfg, *to, !*noSwitch)
	case "undo-move":
		cfg, err := config.Load(args[1:])
		if err != nil {
			return err
		}
		to, err := store.MovedTo(ctx, cfg.Database(), store.Options{})
		if err != nil {
			return err
		}
		if to == "" {
			fmt.Println(store.Describe(cfg.Database()), "wasn't moved; nothing to undo.")
			return nil
		}
		if err := store.UndoMove(ctx, cfg.Database()); err != nil {
			return err
		}
		fmt.Println("Matinee uses", store.Describe(cfg.Database()), "again. Anything that changed in", store.Describe(to), "since the move isn't in it; that database is still there.")
		return nil
	}
	return usage()
}

// moveByHand is `matinee database move`.
func moveByHand(ctx context.Context, cfg config.Config, to string, switchOver bool) error {
	from, err := store.Follow(ctx, cfg.Database(), store.Options{})
	if err != nil {
		return err
	}
	if !store.IsPostgres(from) {
		if _, err := os.Stat(from); err != nil {
			return fmt.Errorf("there's no database at %s: %w", from, err)
		}
	}
	fmt.Println("Moving", store.Describe(from), "to", store.Describe(to))
	fmt.Println("Make sure the Matinee server is stopped: anything it writes during the move isn't copied.")
	db, err := store.OpenWith(from, store.Options{})
	if err != nil {
		return err
	}
	defer db.Close()
	var last time.Time
	report, err := db.MoveTo(ctx, to, func(table string, done, total int64) {
		if time.Since(last) > time.Second || done == total {
			last = time.Now()
			fmt.Printf("\r  copied %d of %d rows", done, total)
		}
	})
	fmt.Println()
	if err != nil {
		return fmt.Errorf("move: %w (the old database wasn't changed)", err)
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', tabwriter.AlignRight)
	fmt.Fprintln(w, "table\trows\t")
	for _, t := range report.Tables {
		if t.Rows > 0 {
			fmt.Fprintf(w, "%s\t%d\t\n", t.Table, t.Rows)
		}
	}
	w.Flush()
	fmt.Printf("Copied %d rows in %d tables in %s; every table has the same number of rows in both.\n", report.Rows, len(report.Tables), report.Took.Round(time.Millisecond))
	if !switchOver {
		fmt.Println("Matinee still uses", store.Describe(from)+".")
		return nil
	}
	if err := db.FinishMove(ctx, from, to, report); err != nil {
		return fmt.Errorf("switch over: %w", err)
	}
	fmt.Println("Matinee uses the new database from its next start. The old one is kept as it was, with a note that sends Matinee to the new one.")
	if store.IsPostgres(to) {
		fmt.Println("Now set MATINEE_DATABASE_URL (or the Helm chart's database.url) to the new database, so the old one can go.")
	} else if filepath.Base(to) == "matinee.db" {
		fmt.Println("Now take MATINEE_DATABASE_URL out of Matinee's settings and set its data folder (MATINEE_DATA_DIR or --data) to " + filepath.Dir(to) + ", so the old database can go.")
	} else {
		fmt.Println("Matinee's settings name a SQLite database as matinee.db in the data folder. Until the new file is that, keep the old database: its note sends Matinee to the new one.")
	}
	return nil
}

type splitArgs struct{ own, others []string }

// splitFlags separates a subcommand's own flags (names) from the server's,
// which say where the database is now.
func splitFlags(args []string, names ...string) splitArgs {
	var out splitArgs
	for i := 0; i < len(args); i++ {
		a := args[i]
		name := strings.TrimLeft(a, "-")
		name, _, hasValue := strings.Cut(name, "=")
		own := false
		for _, n := range names {
			if strings.HasPrefix(a, "-") && name == n {
				own = true
			}
		}
		if !own {
			out.others = append(out.others, a)
			continue
		}
		out.own = append(out.own, a)
		if !hasValue && name != "copy-only" && i+1 < len(args) {
			i++
			out.own = append(out.own, args[i])
		}
	}
	return out
}
