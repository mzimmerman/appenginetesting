package appenginetesting

//The interface returned by GinkgoT().  This covers most of the methods
//in the testing package's T.
type TestingT interface {
	Fail()
	Error(args ...interface{})
	Errorf(format string, args ...interface{})
	FailNow()
	Fatal(args ...interface{})
	Fatalf(format string, args ...interface{})
	Log(args ...interface{})
	Logf(format string, args ...interface{})
	Failed() bool
	Parallel()
	Skip(args ...interface{})
	Skipf(format string, args ...interface{})
	SkipNow()
	Skipped() bool
}
