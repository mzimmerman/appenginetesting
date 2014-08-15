package appenginetesting

import (
	"testing"

	"appengine/datastore"
	"appengine/memcache"
	"appengine/taskqueue"
	"appengine/user"
)

type Entity struct {
	Foo, Bar string
}

func TestLogging(t *testing.T) {
	c, err := NewContext(&Options{Testing: t, Debug: LogChild})
	if err != nil {
		t.Fatalf("NewContext: %v", err)
	}
	defer c.Close()
	if !c.wroteToLog {
		t.Errorf("Child should have logged!")
	}
	c.wroteToLog = false
	c.Errorf("error")
	if !c.wroteToLog {
		t.Errorf("Error should have logged!")
	}
	c.wroteToLog = false
	c.debug = LogInfo
	c.Debugf("debug")
	if c.wroteToLog {
		t.Errorf("Debug should not have logged!")
	}
}

func TestTasks(t *testing.T) {
	t.Parallel()
	c, err := NewContext(&Options{Testing: t, Debug: LogDebug, TaskQueues: []string{"testQueue"}})
	if err != nil {
		t.Fatalf("NewContext: %v", err)
	}
	defer c.Close()

	task := taskqueue.NewPOSTTask("/post", map[string][]string{})
	_, err = taskqueue.Add(c, task, "testQueue")
	if err != nil {
		t.Fatalf("Could not add task to queue")
	}
	stats, err := taskqueue.QueueStats(c, []string{"testQueue"}, 0) // fetch all of them
	if err != nil {
		t.Fatalf("Could not get taskqueue statistics")
	}
	t.Logf("TaskStatistics = %#v", stats)
	if len(stats) == 0 {
		t.Fatalf("Queue statistics are empty")
	} else if stats[0].Tasks != 1 {
		t.Fatalf("Could not find the task we just added")
	}

	err = taskqueue.Purge(c, "testQueue")
	if err != nil {
		t.Fatalf("Could not purge the queue")
	}
	stats, err = taskqueue.QueueStats(c, []string{"testQueue"}, 0) // fetch all of them
	if len(stats) == 0 {
		t.Fatalf("Queue statistics are empty")
	}
	if stats[0].Tasks != 0 {
		t.Fatalf("Purge command not successful")
	}

	tasks := []*taskqueue.Task{
		taskqueue.NewPOSTTask("/post1", map[string][]string{}),
		taskqueue.NewPOSTTask("/post2", map[string][]string{}),
	}
	_, err = taskqueue.AddMulti(c, tasks, "testQueue")
	if err != nil {
		t.Fatalf("Could not add bulk tasklist to queue")
	}
	stats, err = taskqueue.QueueStats(c, []string{"testQueue"}, 0) // fetch all of them
	if err != nil {
		t.Fatalf("Could not get taskqueue statistics")
	}
	if len(stats) == 0 {
		t.Fatalf("Could not find the tasks we just added")
	} else if stats[0].Tasks != 2 {
		t.Fatalf("Could not find the tasks we just added")
	}
}

func TestNamespace(t *testing.T) {
	t.Parallel()
	c, err := NewContext(&Options{Testing: t, Debug: LogDebug})
	if err != nil {
		t.Fatalf("NewContext: %v", err)
	}
	defer c.Close()

	c.CurrentNamespace("private")
	it := &memcache.Item{
		Key:   "foo",
		Value: []byte("value"),
	}
	err = memcache.Set(c, it)
	if err != nil {
		t.Fatalf("Set err = %v", err)
	}
	it, err = memcache.Get(c, "foo")
	if err != nil {
		t.Fatalf("Get err = %v; want no error", err)
	}
	if string(it.Value) != "value" {
		t.Fatalf("got Item.Value = %q; want %q", string(it.Value), "value")
	}

	// now use the default Namespace
	c.CurrentNamespace("")
	_, err = memcache.Get(c, "foo")
	if err != memcache.ErrCacheMiss {
		t.Fatalf("memcache had an entry")
	}
	err = memcache.Set(c, it)
	if err != nil {
		t.Fatalf("Set err = %v", err)
	}
	it, err = memcache.Get(c, "foo")
	if err != nil {
		t.Fatalf("Get err = %v; want no error", err)
	}
	if string(it.Value) != "value" {
		t.Fatalf("got Item.Value = %q; want %q", string(it.Value), "value")
	}
}

func TestContext(t *testing.T) {
	t.Parallel()
	c, err := NewContext(&Options{Testing: t, Debug: LogDebug})
	if err != nil {
		t.Fatalf("NewContext: %v", err)
	}
	defer c.Close()

	_, err = memcache.Get(c, "foo")
	if err != memcache.ErrCacheMiss {
		t.Fatalf("Get err = %v; want ErrCacheMiss", err)
	}

	it := &memcache.Item{
		Key:   "foo",
		Value: []byte("value"),
	}
	err = memcache.Set(c, it)
	if err != nil {
		t.Fatalf("Set err = %v", err)
	}
	it, err = memcache.Get(c, "foo")
	if err != nil {
		t.Fatalf("Get err = %v; want no error", err)
	}
	if string(it.Value) != "value" {
		t.Errorf("got Item.Value = %q; want %q", string(it.Value), "value")
	}

	e := &Entity{Foo: "foo", Bar: "bar"}
	k := datastore.NewKey(c, "Entity", "", 1, nil)
	_, err = datastore.Put(c, k, e)
	if err != nil {
		t.Fatalf("datastore.Put: %v", err)
	}
	u := user.Current(c)
	if u != nil {
		t.Fatalf("User should not be not logged in!")
	}
	c.Login(&user.User{Email: "user@host.com", Admin: false})
	u = user.Current(c)
	if u == nil {
		t.Fatalf("User should be logged in!")
	}
	id1 := u.ID
	c.Logout()
	u = user.Current(c)
	if u != nil {
		t.Fatalf("User should not be not logged in!")
	}
	c.Login(&user.User{Email: "differentuser@host.com", Admin: false})
	u = user.Current(c)
	if u == nil {
		t.Fatalf("User should be logged in!")
	}
	if id1 == u.ID {
		t.Fatalf("User IDs should be unique")
	}
}
