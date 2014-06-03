package appenginetesting

import (
	"fmt"

	"appengine/datastore"
	"appengine/user"
)

func ExampleNewContext_user() {
	// Create mocked context.
	c, err := NewContext(nil)
	if err != nil {
		fmt.Println("initilizing context:", err)
		return
	}

	// Close the context when we are done.
	defer c.Close()

	// Log a user in.
	c.Login(&user.User{Email: "test@example.com", Admin: true})

	// Get the user.
	u := user.Current(c)
	if u == nil {
		fmt.Println("we didn't get a user!")
		return
	}

	fmt.Println("Email:", u.Email)
	fmt.Println("Admin:", u.Admin)

	// Output:
	// Email: test@example.com
	// Admin: true
}

func ExampleNewContext_datastore() {
	// Create mocked context.
	c, err := NewContext(nil)
	if err != nil {
		fmt.Println("initilizing context:", err)
		return
	}

	// Close the context when we are done.
	defer c.Close()

	// Get an element from the datastore.
	k := datastore.NewKey(c, "Entity", "stringID", 0, nil)
	e := ""
	if err := datastore.Get(c, k, &e); err != nil {
		fmt.Println("datastore get:", err)
		return
	}

	// Put an element in the datastore.
	if _, err := datastore.Put(c, k, &e); err != nil {
		fmt.Println("datastore put:", err)
		return
	}
}
