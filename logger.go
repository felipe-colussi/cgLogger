package cgLogger

import (
	"context"
	"errors"
	"fmt"
	lg "gorm.io/gorm/logger"
	"gorm.io/gorm/utils"
	"log"
	"os"
	"time"
)

var ErrRecordNotFound = errors.New("record not found")

// Colors
const (
	Reset       = "\033[0m"
	Red         = "\033[31m"
	Green       = "\033[32m"
	Yellow      = "\033[33m"
	Blue        = "\033[34m"
	Magenta     = "\033[35m"
	Cyan        = "\033[36m"
	White       = "\033[37m"
	BlueBold    = "\033[34;1m"
	MagentaBold = "\033[35;1m"
	RedBold     = "\033[31;1m"
	YellowBold  = "\033[33;1m"
)

// GormInfos are the data passed to the custom functions
type GormInfos struct {
	Location      string
	AffectedRows  int64
	QueryDuration float64
	Sql           string
	Err           error
}

// Writer log writer interface
type Writer interface {
	Printf(string, ...interface{})
}

type Config struct {
	SlowThreshold             time.Duration
	Colorful                  bool
	IgnoreRecordNotFoundError bool
	LogLevel                  lg.LogLevel
}

// CInterface customLogger interface
type CInterface interface {
	LogMode(lg.LogLevel) lg.Interface
	Info(context.Context, string, ...interface{})
	Warn(context.Context, string, ...interface{})
	Error(context.Context, string, ...interface{})
	Trace(ctx context.Context, begin time.Time, fc func() (sql string, rowsAffected int64), err error)
	AlwaysTrigger(f func(g GormInfos)) CInterface
	SlowTrigger(f func(g GormInfos), duration time.Duration) CInterface
	ErrorTrigger(f func(g GormInfos)) CInterface
	ConsiderNotFound(b bool) CInterface
}

var (
	Default = New(
		log.New(os.Stdout, "\r\n", log.LstdFlags), Config{
			SlowThreshold: 200 * time.Millisecond,
			LogLevel:      lg.Warn,
			Colorful:      true,
		},
	)
)

// New is a "Copy" of the original logger except it implements the new methods.
func New(writer Writer, config Config) CInterface {
	var (
		infoStr      = "%s\n[info] "
		warnStr      = "%s\n[warn] "
		errStr       = "%s\n[error] "
		traceStr     = "%s\n[%.3fms] [rows:%v] %s"
		traceWarnStr = "%s %s\n[%.3fms] [rows:%v] %s"
		traceErrStr  = "%s %s\n[%.3fms] [rows:%v] %s"
	)

	if config.Colorful {
		infoStr = Green + "%s\n" + Reset + Green + "[info] " + Reset
		warnStr = BlueBold + "%s\n" + Reset + Magenta + "[warn] " + Reset
		errStr = Magenta + "%s\n" + Reset + Red + "[error] " + Reset
		traceStr = Green + "%s\n" + Reset + Yellow + "[%.3fms] " + BlueBold + "[rows:%v]" + Reset + " %s"
		traceWarnStr = Green + "%s " + Yellow + "%s\n" + Reset + RedBold + "[%.3fms] " + Yellow + "[rows:%v]" + Magenta + " %s" + Reset
		traceErrStr = RedBold + "%s " + MagentaBold + "%s\n" + Reset + Yellow + "[%.3fms] " + BlueBold + "[rows:%v]" + Reset + " %s"
	}

	return &customLogger{
		Writer:       writer,
		Config:       config,
		infoStr:      infoStr,
		warnStr:      warnStr,
		errStr:       errStr,
		traceStr:     traceStr,
		traceWarnStr: traceWarnStr,
		traceErrStr:  traceErrStr,
	}
}

// customLogger have Execution so it can add functions in the logger.
type customLogger struct {
	Writer
	Config
	Execution
	infoStr, warnStr, errStr            string
	traceStr, traceErrStr, traceWarnStr string
}

// SetSlowSqlThreshold Set the slowSqlThreshold to be shown on warns
// is the same as the original logger .
func (l *customLogger) SetSlowSqlThreshold(t time.Duration) {
	l.Config.SlowThreshold = t
}

// LogMode This function set the LogMode and returns a Gorm - Interface.
// This wil block t he edition of the Triggers.
// The edition Should be block cause changing it isn't concurrency safe
func (l *customLogger) LogMode(level lg.LogLevel) lg.Interface {
	newLogger := *l
	newLogger.LogLevel = level

	return &newLogger
}

// FixTriggers will set the Gorm Interface and block all Trigger functions.
func (l *customLogger) FixTriggers() lg.Interface {
	return l
}

// AlwaysTrigger will trigger during all sql that use this logger.
func (l *customLogger) AlwaysTrigger(f func(g GormInfos)) CInterface {
	l.always = f
	return l
}

// SlowTrigger will trigger if the query took more than the duration
func (l *customLogger) SlowTrigger(f func(g GormInfos), duration time.Duration) CInterface {
	l.warns = f
	l.slowSqlTrigger = duration
	return l
}

// ErrorTrigger will trigger if gorm presents an error.  By default this will ignore ErrRecordNotFound
func (l *customLogger) ErrorTrigger(f func(g GormInfos)) CInterface {
	l.errors = f
	return l
}

// ConsiderNotFound  if true will consider ErrRecordNotFound as an error to invoke the ErrorsTrigger
func (l *customLogger) ConsiderNotFound(b bool) CInterface {
	l.considerRecordNotFoundError = b
	return l
}

/*******************************
*	COPY OF THE DEFAULT LOGGER *
*******************************/

// Info print info
func (l customLogger) Info(ctx context.Context, msg string, data ...interface{}) {
	if l.LogLevel >= lg.Info {
		l.Printf(l.infoStr+msg, append([]interface{}{utils.FileWithLineNum()}, data...)...)
	}
}

// Warn print warn messages
func (l customLogger) Warn(ctx context.Context, msg string, data ...interface{}) {
	if l.LogLevel >= lg.Warn {
		l.Printf(l.warnStr+msg, append([]interface{}{utils.FileWithLineNum()}, data...)...)
	}
}

// Error print error messages
func (l customLogger) Error(ctx context.Context, msg string, data ...interface{}) {
	if l.LogLevel >= lg.Error {
		l.Printf(l.errStr+msg, append([]interface{}{utils.FileWithLineNum()}, data...)...)
	}
}

/* END OF THE COPY */

// Trace print sql message and execute in this order:
// AlwaysTrigger
// SlowTrigger
// ErrorTrigger
func (l customLogger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	sql, rows := fc()
	elapsed := time.Since(begin)
	slowSql := elapsed > l.SlowThreshold && l.SlowThreshold != 0

	g := GormInfos{
		Location:      utils.FileWithLineNum(),
		AffectedRows:  rows,
		QueryDuration: float64(elapsed.Nanoseconds()) / 1e6,
		Sql:           sql,
		Err:           err,
	}

	if l.always != nil {
		l.always(g)
	}

	if l.slowSqlTrigger != 0 && elapsed > l.slowSqlTrigger && l.warns != nil {
		l.warns(g)
	}

	if err != nil && (!errors.Is(err, ErrRecordNotFound) || l.considerRecordNotFoundError) && l.errors != nil {
		l.errors(g)
	}

	if l.LogLevel <= lg.Silent {
		return
	}

	switch {
	case err != nil && l.LogLevel >= lg.Error && (!errors.Is(err, ErrRecordNotFound) || !l.IgnoreRecordNotFoundError):
		if rows == -1 {
			l.Printf(l.traceErrStr, utils.FileWithLineNum(), err, float64(elapsed.Nanoseconds())/1e6, "-", sql)
		} else {
			l.Printf(l.traceErrStr, utils.FileWithLineNum(), err, float64(elapsed.Nanoseconds())/1e6, rows, sql)
		}
	case slowSql && l.LogLevel >= lg.Warn:
		slowLog := fmt.Sprintf("SLOW SQL >= %v", l.SlowThreshold)
		if rows == -1 {
			l.Printf(l.traceWarnStr, utils.FileWithLineNum(), slowLog, float64(elapsed.Nanoseconds())/1e6, "-", sql)
		} else {
			l.Printf(l.traceWarnStr, utils.FileWithLineNum(), slowLog, float64(elapsed.Nanoseconds())/1e6, rows, sql)
		}
	case l.LogLevel == lg.Info:
		if rows == -1 {
			l.Printf(l.traceStr, utils.FileWithLineNum(), float64(elapsed.Nanoseconds())/1e6, "-", sql)
		} else {
			l.Printf(l.traceStr, utils.FileWithLineNum(), float64(elapsed.Nanoseconds())/1e6, rows, sql)
		}
	}
}

// Execution contains the Methods to be hold
type Execution struct {
	always                      func(g GormInfos)
	warns                       func(g GormInfos)
	slowSqlTrigger              time.Duration
	errors                      func(f GormInfos)
	considerRecordNotFoundError bool
}
