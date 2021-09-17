package log

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/natefinch/lumberjack"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type Options struct {
	LogFileDir    string // 文件保存地方
	AppName       string // 日志文件前缀
	FileName      string
	ErrorFileName string
	WarnFileName  string
	InfoFileName  string
	DebugFileName string
	Level         zapcore.Level // 日志等级
	MaxSize       int           // 日志文件小大（M）
	MaxBackups    int           // 最多存在多少个切片文件
	MaxAge        int           // 保存的最大天数
	Development   bool          // 是否是开发模式
	zap.Config
	Merge bool // 是否合并日志
}

type Option func(options *Options)
type LogOutputFunc func(msg string, fields ...zap.Field)
type LogFormatFunc func(msg string, args ...interface{})

var (
	l  *Logger
	sp = string(filepath.Separator)

	fileWs    zapcore.WriteSyncer       // 文件输出
	consoleWs = zapcore.Lock(os.Stdout) // 控制台输出

	Debug func(msg string, fields ...zap.Field)
	Info  func(msg string, fields ...zap.Field)
	Warn  func(msg string, fields ...zap.Field)
	Error func(msg string, fields ...zap.Field)

	// Debugf LogFormatFunc
	// Infof  LogFormatFunc
	// Warnf  LogFormatFunc
	// Errorf LogFormatFunc
)

func init() {
	core := zapcore.NewCore(
		zapcore.NewConsoleEncoder(zapcore.EncoderConfig{
			TimeKey:        "T",
			LevelKey:       "L",
			NameKey:        "N",
			CallerKey:      "C",
			MessageKey:     "M",
			StacktraceKey:  "S",
			LineEnding:     zapcore.DefaultLineEnding,
			EncodeLevel:    zapcore.CapitalColorLevelEncoder,
			EncodeTime:     timeEncoder,
			EncodeDuration: zapcore.StringDurationEncoder,
			EncodeCaller:   zapcore.ShortCallerEncoder,
		}),
		zapcore.NewMultiWriteSyncer(zapcore.AddSync(os.Stdout)),
		zap.DebugLevel,
	)
	logger := zap.New(core)
	Debug = logger.Debug
	Info = logger.Info
	Warn = logger.Warn
	Error = logger.Error
}

type Logger struct {
	*zap.Logger
	sync.RWMutex
	Opts      *Options `json:"opts"`
	zapConfig zap.Config
	inited    bool
}

func NewLogger(opt ...Option) *zap.Logger {
	l = &Logger{}
	l.Lock()
	defer l.Unlock()
	if l.inited {
		l.Info("[NewLogger] logger Inited")
		return nil
	}
	l.Opts = &Options{
		LogFileDir: "",
		AppName:    "app",
		FileName:   ".log",
		Level:      zapcore.DebugLevel,
		MaxSize:    100,
		MaxBackups: 60,
		MaxAge:     30,
	}
	if l.Opts.LogFileDir == "" {
		l.Opts.LogFileDir, _ = filepath.Abs(filepath.Dir(filepath.Join(".")))
		l.Opts.LogFileDir += sp + "logs" + sp
	}
	if l.Opts.Development {
		l.zapConfig = zap.NewDevelopmentConfig()
		l.zapConfig.EncoderConfig.EncodeTime = timeEncoder
	} else {
		l.zapConfig = zap.NewProductionConfig()
		l.zapConfig.EncoderConfig.EncodeTime = timeEncoder
	}
	if l.Opts.OutputPaths == nil || len(l.Opts.OutputPaths) == 0 {
		l.zapConfig.OutputPaths = []string{"stdout"}
	}
	if l.Opts.ErrorOutputPaths == nil || len(l.Opts.ErrorOutputPaths) == 0 {
		l.zapConfig.OutputPaths = []string{"stderr"}
	}
	for _, fn := range opt {
		fn(l.Opts)
	}
	l.zapConfig.DisableStacktrace = true
	l.zapConfig.Level.SetLevel(l.Opts.Level)
	l.init()
	l.inited = true
	l.Info("[NewLogger] success")

	Info = l.Logger.Info
	Debug = l.Logger.Debug
	Warn = l.Logger.Warn
	Error = l.Logger.Error

	return l.Logger
}

func (l *Logger) init() {
	l.setSyncers()
	var err error
	l.Logger, err = l.zapConfig.Build(l.cores())
	if err != nil {
		panic(err)
	}
	defer l.Logger.Sync()
}

func (l *Logger) setSyncers() {
	f := func(fN string) zapcore.WriteSyncer {
		fileName := l.Opts.LogFileDir + sp + l.Opts.AppName + "-" + fN
		if len(fN) == len(".log") {
			fileName = l.Opts.LogFileDir + sp + l.Opts.AppName + fN
		}
		return zapcore.AddSync(&lumberjack.Logger{
			Filename:   fileName,
			MaxSize:    l.Opts.MaxSize,
			MaxBackups: l.Opts.MaxBackups,
			MaxAge:     l.Opts.MaxAge,
			Compress:   true,
			LocalTime:  true,
		})
	}
	fileWs = f(l.Opts.FileName)
}

func WithMaxSize(MaxSize int) Option {
	return func(option *Options) {
		option.MaxSize = MaxSize
	}
}

func WithMaxBackups(MaxBackups int) Option {
	return func(option *Options) {
		option.MaxBackups = MaxBackups
	}
}

func WithMaxAge(MaxAge int) Option {
	return func(option *Options) {
		option.MaxAge = MaxAge
	}
}

func WithLogFileDir(LogFileDir string) Option {
	return func(option *Options) {
		option.LogFileDir = LogFileDir
	}
}

func WithAppName(AppName string) Option {
	return func(option *Options) {
		option.AppName = AppName
	}
}

func WithLevel(Level zapcore.Level) Option {
	return func(option *Options) {
		option.Level = Level
	}
}

func WithFileName(FileName string) Option {
	return func(option *Options) {
		option.FileName = FileName
	}
}

func WithErrorFileName(ErrorFileName string) Option {
	return func(option *Options) {
		option.ErrorFileName = ErrorFileName
	}
}

func WithWarnFileName(WarnFileName string) Option {
	return func(option *Options) {
		option.WarnFileName = WarnFileName
	}
}

func WithInfoFileName(InfoFileName string) Option {
	return func(option *Options) {
		option.InfoFileName = InfoFileName
	}
}
func WithDebugFileName(DebugFileName string) Option {
	return func(option *Options) {
		option.DebugFileName = DebugFileName
	}
}

func WithDevelopment(Development bool) Option {
	return func(option *Options) {
		option.Development = Development
	}
}

func (l *Logger) cores() zap.Option {
	fileEncoder := zapcore.NewJSONEncoder(l.zapConfig.EncoderConfig)

	encoderConfig := zap.NewDevelopmentEncoderConfig()
	encoderConfig.EncodeTime = timeEncoder
	encoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	consoleEncoder := zapcore.NewConsoleEncoder(encoderConfig)

	filePriority := zap.LevelEnablerFunc(func(lvl zapcore.Level) bool {
		return lvl >= l.zapConfig.Level.Level()
	})

	cores := []zapcore.Core{zapcore.NewCore(fileEncoder, fileWs, filePriority)}
	if l.Opts.Development {
		cores = append(cores, []zapcore.Core{zapcore.NewCore(consoleEncoder, consoleWs, filePriority)}...)
	}
	return zap.WrapCore(func(c zapcore.Core) zapcore.Core {
		return zapcore.NewTee(cores...)
	})
}

func timeEncoder(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
	enc.AppendString(t.Format("2006-01-02 15:04:05.000"))
}

// func timeUnixNano(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
// 	enc.AppendInt64(t.UnixNano() / 1e6)
// }

func SetLevel(level zapcore.Level) {
	if l == nil {
		return
	}
	l.Opts.Level = level
	l.zapConfig.Level.SetLevel(l.Opts.Level)
	l.Info("[SetLevel] success")
}

func Sync() {
	if l == nil {
		return
	}
	l.Logger.Sync()
}

// ------------------------------------------------
// ------------------------------------------------

// catch exception for no panic
func CatchException() {
	if err := recover(); err != nil {
		logfile, err2 := os.OpenFile(newDumpFile(), os.O_RDWR|os.O_APPEND|os.O_CREATE, os.ModePerm)
		if err2 != nil {
			fmt.Println(err2)
			return
		}

		defer logfile.Close()
		logger := log.New(logfile, "", log.Ldate|log.Lmicroseconds|log.Lshortfile)
		logger.SetFlags(0)

		strLog := fmt.Sprintf(`
===============================================================================
TIME: %v
EXCEPTION: %#v
===============================================================================		
%s`,
			time.Now(),
			err,
			string(debug.Stack()))

		logger.Println(strLog)
		fmt.Println(strLog)
	}
}

// generate dumpfile
func newDumpFile() string {
	var isFileExist = func(fn string) bool {
		finfo, err := os.Stat(fn)
		if err != nil {
			return false
		}
		if finfo.IsDir() {
			return false
		}
		return true
	}

	binDir, _ := filepath.Abs(filepath.Dir(os.Args[0]))
	now := time.Now()
	filename := fmt.Sprintf("exceptions.%02d_%02d_%02d", now.Hour(), now.Minute(), now.Second())
	dir := fmt.Sprintf("%s/exceptions/%04d-%02d-%02d/", strings.TrimRight(binDir, "/"), now.Year(), int(now.Month()), now.Day())
	os.MkdirAll(dir, os.ModePerm)
	fn := fmt.Sprintf("%s%s.log", dir, filename)
	if !isFileExist(fn) {
		return fn
	}

	n := 1
	for {
		fn = fmt.Sprintf("%s%s_%d.log", dir, filename, n)
		if !isFileExist(fn) {
			break
		}
		n += 1
	}
	return fn
}
