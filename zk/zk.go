/*
   Copyright 2014 Outbrain Inc.

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

// zk provides with higher level commands over the lower level zookeeper connector
package zk

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/outbrain/golib/log"
	"github.com/samuel/go-zookeeper/zk"
	"math"
	gopath "path"
	"sort"
	"strconv"
	"strings"
	"time"
)

var servers []string
var authScheme string
var authExpression []byte

var flags int32 = int32(0)

// We assume complete access to all
var acl []zk.ACL = zk.WorldACL(zk.PermAll)

// SetServers sets the list of servers for the zookeeper client to connect to.
// Each element in the array should be in either of following forms:
// - "servername"
// - "servername:port"
func SetServers(serversArray []string) {
	servers = serversArray
}

func SetAuth(scheme string, auth []byte) {
	log.Debug("Setting Auth ")
	authScheme = scheme
	authExpression = auth
}

// Returns acls
func BuildACL(authScheme string, user string, pwd string, acls string) (perms []zk.ACL, err error) {
	aclsList := strings.Split(acls, ",")
	for _, elem := range aclsList {
		acl, err := strconv.ParseInt(elem, 10, 32)
		if err != nil {
			break
		}
		perm := zk.DigestACL(int32(acl), user, pwd)
		perms = append(perms, perm[0])
	}
	return perms, err
}

type infoLogger struct{}

func (_ infoLogger) Printf(format string, a ...interface{}) {
	log.Infof(format, a...)
}

// connect
func connect() (*zk.Conn, error) {
	zk.DefaultLogger = &infoLogger{}
	conn, _, err := zk.Connect(servers, time.Second)
	if err == nil && authScheme != "" {
		log.Debugf("Add Auth %s %s", authScheme, authExpression)
		err = conn.AddAuth(authScheme, authExpression)
	}

	return conn, err
}

// Exists returns true when the given path exists
func Exists(path string) (bool, error) {
	connection, err := connect()
	if err != nil {
		return false, err
	}
	defer connection.Close()

	exists, _, err := connection.Exists(path)
	return exists, err
}

// Get returns value associated with given path, or error if path does not exist
func Get(path string) ([]byte, error) {
	connection, err := connect()
	if err != nil {
		return []byte{}, err
	}
	defer connection.Close()

	data, _, err := connection.Get(path)
	return data, err
}

func GetACL(path string) (data []string, err error) {
	connection, err := connect()
	if err != nil {
		return nil, err
	}
	defer connection.Close()

	perms, _, err := connection.GetACL(path)
	return aclsToString(perms), err
}

func aclsToString(acls []zk.ACL) (result []string) {
	for _, acl := range acls {
		var buffer bytes.Buffer

		buffer.WriteString(fmt.Sprintf("%v:%v:", acl.Scheme, acl.ID))

		if acl.Perms&zk.PermCreate != 0 {
			buffer.WriteString("c")
		}
		if acl.Perms&zk.PermDelete != 0 {
			buffer.WriteString("d")
		}
		if acl.Perms&zk.PermRead != 0 {
			buffer.WriteString("r")
		}
		if acl.Perms&zk.PermWrite != 0 {
			buffer.WriteString("w")
		}
		if acl.Perms&zk.PermAdmin != 0 {
			buffer.WriteString("a")
		}
		result = append(result, buffer.String())
	}
	return result
}

// Children returns sub-paths of given path, optionally empty array, or error if path does not exist
func Children(path string) ([]string, error) {
	connection, err := connect()
	if err != nil {
		return []string{}, err
	}
	defer connection.Close()

	children, _, err := connection.Children(path)
	return children, err
}

// childrenRecursiveInternal: internal implementation of recursive-children query.
func childrenRecursiveInternal(connection *zk.Conn, path string, incrementalPath string) ([]string, error) {
	children, _, err := connection.Children(path)
	if err != nil {
		return children, err
	}
	sort.Sort(sort.StringSlice(children))
	recursiveChildren := []string{}
	for _, child := range children {
		incrementalChild := gopath.Join(incrementalPath, child)
		recursiveChildren = append(recursiveChildren, incrementalChild)
		log.Debugf("incremental child: %+v", incrementalChild)
		incrementalChildren, err := childrenRecursiveInternal(connection, gopath.Join(path, child), incrementalChild)
		if err != nil {
			return children, err
		}
		recursiveChildren = append(recursiveChildren, incrementalChildren...)
	}
	return recursiveChildren, err
}

// ChildrenRecursive returns list of all descendants of given path (optionally empty), or error if the path
// does not exist.
// Every element in result list is a relative subpath for the given path.
func ChildrenRecursive(path string) ([]string, error) {
	connection, err := connect()
	if err != nil {
		return []string{}, err
	}
	defer connection.Close()

	result, err := childrenRecursiveInternal(connection, path, "")
	return result, err
}

// createInternal: create a new path
func createInternal(connection *zk.Conn, path string, data []byte, acl []zk.ACL, force bool, flags int32) (string, error) {
	if path == "/" {
		return "/", nil
	}

	log.Debugf("creating: %s", path)
	attempts := 0
	for {
		attempts += 1
		returnValue, err := connection.Create(path, data, flags, acl)
		log.Debugf("create status for %s: %s, %+v", path, returnValue, err)
		if err != nil && force && attempts < 2 {
			returnValue, err = createInternal(connection, gopath.Dir(path), []byte("zookeepercli auto-generated"), acl, force, flags)
		} else {
			return returnValue, err
		}
	}
	return "", nil
}

// createInternalWithACL: create a new path with acl
func createInternalWithACL(connection *zk.Conn, path string, data []byte, force bool, perms []zk.ACL, flags int32) (string, error) {
	if path == "/" {
		return "/", nil
	}
	log.Debugf("creating: %s with acl ", path)
	attempts := 0
	for {
		attempts += 1
		returnValue, err := connection.Create(path, data, flags, perms)
		log.Debugf("create status for %s: %s, %+v", path, returnValue, err)
		if err != nil && force && attempts < 2 {
			returnValue, err = createInternalWithACL(connection, gopath.Dir(path), []byte("zookeepercli auto-generated"), force, perms, flags)
		} else {
			return returnValue, err
		}
	}
	return "", nil
}

// Create will create a new path, or exit with error should the path exist.
// The "force" param controls the behavior when path's parent directory does not exist.
// When "force" is false, the function returns with error/ When "force" is true, it recursively
// attempts to create required parent directories.
func Create(path string, data []byte, aclstr string, force bool, flags int32) (string, error) {
	connection, err := connect()
	if err != nil {
		return "", err
	}
	defer connection.Close()

	if len(aclstr) > 0 {
		acl, err = parseACLString(aclstr)
		if err != nil {
			return "", err
		}
	}

	return createInternal(connection, path, data, acl, force, flags)
}

func CreateWithACL(path string, data []byte, force bool, perms []zk.ACL, flags int32) (string, error) {
	connection, err := connect()
	if err != nil {
		return "", err
	}
	defer connection.Close()

	return createInternalWithACL(connection, path, data, force, perms, flags)
}

// Set updates a value for a given path, or returns with error if the path does not exist
func Set(path string, data []byte) (*zk.Stat, error) {
	connection, err := connect()
	if err != nil {
		return nil, err
	}
	defer connection.Close()

	return connection.Set(path, data, -1)
}

// updates the ACL on a given path
func SetACL(path string, aclstr string, force bool) (string, error) {
	connection, err := connect()
	if err != nil {
		return "", err
	}
	defer connection.Close()

	acl, err := parseACLString(aclstr)
	if err != nil {
		return "", err
	}

	if force {
		exists, _, err := connection.Exists(path)
		if err != nil {
			return "", err
		}

		if !exists {
			return createInternal(connection, path, []byte(""), acl, force, flags)
		}
	}

	_, err = connection.SetACL(path, acl, -1)
	return path, err
}

func parseACLString(aclstr string) (acl []zk.ACL, err error) {
	aclsList := strings.Split(aclstr, ",")
	for _, entry := range aclsList {
		parts := strings.Split(entry, ":")
		var scheme, id string
		var perms int32
		if len(parts) > 3 && parts[0] == "digest" {
			scheme = parts[0]
			id = fmt.Sprintf("%s:%s", parts[1], parts[2])
			perms, err = parsePermsString(parts[3])
		} else {
			scheme, id = parts[0], parts[1]
			perms, err = parsePermsString(parts[2])
		}

		if err == nil {
			perm := zk.ACL{Scheme: scheme, ID: id, Perms: perms}
			acl = append(acl, perm)
		}
	}
	return acl, err
}

func parsePermsString(permstr string) (perms int32, err error) {
	if x, e := strconv.ParseFloat(permstr, 64); e == nil {
		perms = int32(math.Min(x, 31))
	} else {
		for _, rune := range strings.Split(permstr, "") {
			switch rune {
			case "r":
				perms |= zk.PermRead
				break
			case "w":
				perms |= zk.PermWrite
				break
			case "c":
				perms |= zk.PermCreate
				break
			case "d":
				perms |= zk.PermDelete
				break
			case "a":
				perms |= zk.PermAdmin
				break
			default:
				err = errors.New("invalid ACL string specified")
			}

			if err != nil {
				break
			}
		}
	}
	return perms, err
}

// Delete removes a path entry. It exits with error if the path does not exist, or has subdirectories.
func Delete(path string) error {
	connection, err := connect()
	if err != nil {
		return err
	}
	defer connection.Close()

	return connection.Delete(path, -1)
}

// Delete recursive if has subdirectories.
func DeleteRecursive(path string) error {
	result, err := ChildrenRecursive(path)
	if err != nil {
		log.Fatale(err)
	}

	for i := len(result) - 1; i >= 0; i-- {
		znode := path + "/" + result[i]
		if err = Delete(znode); err != nil {
			log.Fatale(err)
		}
	}

	return Delete(path)
}
