package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	ign3types "github.com/coreos/ignition/v2/config/v3_2/types"
	"github.com/golang/glog"
	"github.com/google/rpmpack"

	mcfgv1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"
	ctrlcommon "github.com/openshift/machine-config-operator/pkg/controller/common"
)

func main() {

	var onceFrom string
	var outputRPM string

	flag.Set("logtostderr", "true")
	flag.Set("stderrthreshold", "WARNING")
	flag.Set("v", "2")

	flag.StringVar(&onceFrom, "config", "", "Config file ign/machineconfig to read")
	flag.StringVar(&outputRPM, "output", "", "RPM target file to write")
	flag.Parse()

	fileName := strings.TrimSuffix(onceFrom, filepath.Ext(onceFrom))
	fileName = filepath.Base(fileName)

	var err error

	// use the library functions to figure out what this is
	configi, contentFrom, err := senseAndLoadOnceFrom(onceFrom)
	if err != nil {
		glog.Fatalf("Unable to decipher onceFrom config type: %s", err)
	}
	_ = contentFrom
	hostname, _ := os.Hostname()

	packedRPM, err := rpmpack.NewRPM(rpmpack.RPMMetaData{
		Name:        fileName,
		Version:     "1",
		Release:     "2",
		Summary:     "A package packed from " + onceFrom,
		Description: "This is a machine-packed RPM packed by 'ignition2rpm'",
		BuildTime:   time.Now(),
		Packager:    "MCO ignition2rpm",
		Vendor:      "RedHat OpenShift",
		Licence:     "ASL 2.0",
		BuildHost:   hostname,
	},
	)
	if err != nil {
		glog.Fatalf("Failed to create new RPM file: %s", err)
	}

	// have to process the envelope if it's machineconfig, otherwise we don't
	switch c := configi.(type) {
	case ign3types.Config:
		err = Ign2Rpm(packedRPM, &c)
		if err != nil {
			panic(err)
		}
	case mcfgv1.MachineConfig:

	}

	f, err := os.Create(outputRPM)
	if err != nil {
		panic(err)
	}

	if err := packedRPM.Write(f); err != nil {
		panic(err)
	}

	glog.Infof("Wrote %s", outputRPM)

}

type onceFromOrigin int

const (
	onceFromUnknownConfig onceFromOrigin = iota
	onceFromLocalConfig
	onceFromRemoteConfig
)

func senseAndLoadOnceFrom(onceFrom string) (interface{}, onceFromOrigin, error) {
	var (
		content     []byte
		contentFrom onceFromOrigin
	)
	// Read the content from a remote endpoint if requested
	/* #nosec */
	if strings.HasPrefix(onceFrom, "http://") || strings.HasPrefix(onceFrom, "https://") {
		contentFrom = onceFromRemoteConfig
		resp, err := http.Get(onceFrom)
		if err != nil {
			return nil, contentFrom, err
		}
		defer resp.Body.Close()
		// Read the body content from the request
		content, err = ioutil.ReadAll(resp.Body)
		if err != nil {
			return nil, contentFrom, err
		}

	} else {
		// Otherwise read it from a local file
		contentFrom = onceFromLocalConfig
		absoluteOnceFrom, err := filepath.Abs(filepath.Clean(onceFrom))
		if err != nil {
			return nil, contentFrom, err
		}
		content, err = ioutil.ReadFile(absoluteOnceFrom)
		if err != nil {
			return nil, contentFrom, err
		}
	}

	// Try each supported parser
	ignConfig, err := ctrlcommon.ParseAndConvertConfig(content)
	if err == nil && ignConfig.Ignition.Version != "" {
		glog.V(2).Info("onceFrom file is of type Ignition")
		return ignConfig, contentFrom, nil
	}

	/*
		// Try to parse as a machine config
		mc, err := resourceread.ReadMachineConfigV1(content)
		if err == nil && mc != nil {
			glog.V(2).Info("onceFrom file is of type MachineConfig")
			return *mc, contentFrom, nil
		}*/

	return nil, onceFromUnknownConfig, fmt.Errorf("unable to decipher onceFrom config type: %v", err)
}

func NilMode(obj *int, val uint) uint {
	if obj == nil {
		return val
	}

	octalstring := strconv.FormatInt(int64(*obj), 8)
	octal, _ := strconv.ParseInt(octalstring, 8, 64)
	return uint(octal)

}

func NilString(obj *string, val string) string {
	if obj == nil {
		return val
	}
	return *obj
}

func NilBool(obj *bool, val bool) bool {
	if obj == nil {
		return val
	}
	return *obj
}

// Rewrites file paths for rpm-ostree so they get linked back into the right place.
// This will break the package though for things like RHEL, because it won't be able to find its toys
func RelocateForRpmOstree(fileName string) string {
	//TODO: anchor to the beginning so you don't match /var/usr/local or something like that
	replaced := strings.Replace(fileName, "/usr/local/", "/var/usrlocal/", 1)
	if replaced != fileName {
		glog.Infof("REPLACING: %s %s", fileName, replaced)
	}
	return replaced

}

// Converts ignition to an RPM and returns the RPM
func Ign2Rpm(r *rpmpack.RPM, config *ign3types.Config) error {

	//create the RPM

	packTime := time.Now().Unix()

	// MCO currently support sshkeys
	//yes I'm cheating because I know /var/home becomes /home
	coreUserSSHDir := "/var/home/core/.ssh"
	for _, u := range config.Passwd.Users {
		concatKeys := ""
		if u.Name == "core" {
			glog.Infof("Found the core user, adding authorized_keys")
			for _, key := range u.SSHAuthorizedKeys {
				concatKeys = concatKeys + string(key) + "\n"
			}

		}
		rpmfile := rpmpack.RPMFile{

			Name:  filepath.Join(coreUserSSHDir, "authorized_keys"),
			Body:  []byte(concatKeys),
			Mode:  0644,
			Owner: u.Name,
			Group: u.Name,
			MTime: uint32(packTime),
			Type:  rpmpack.GenericFile,
		}
		r.AddFile(rpmfile)

	}

	// Map from paths in the config to where they resolve for duplicate checking
	for _, d := range config.Storage.Directories {
		glog.Infof("DIR: %s (%d %s) (%d %s) %v\n", d.Path, d.User.ID, d.User.Name, d.Group.ID, d.Group.Name, d.Node)
		//rpm-ostree limits where we can put files
		//but it links some of them back into the right spots
		d.Path = RelocateForRpmOstree(d.Path)
		rpmfile := rpmpack.RPMFile{
			Name: d.Path,
			//Body:  []byte(*d.Contents.Source),
			// The Nil<whatever> functions insulate us from nil pointers and return a default value
			Mode:  NilMode(d.Mode, 0755),
			Owner: NilString(d.User.Name, "root"),
			Group: NilString(d.Group.Name, "root"),
			MTime: uint32(packTime),
			Type:  rpmpack.GenericFile,
		}
		//tell the rpm library it's a directory. Yes, this could be better
		rpmfile.Mode |= 040000
		r.AddFile(rpmfile)

	}

	for _, f := range config.Storage.Files {
		glog.Infof("FILE: %s\n", f.Path)

		//rpm-ostree limits where we can put files
		//but it links some of them back into the right spots
		f.Path = RelocateForRpmOstree(f.Path)
		rpmfile := rpmpack.RPMFile{
			Name:  f.Path,
			Body:  []byte(*f.Contents.Source),
			Mode:  NilMode(f.Mode, 0755),
			Owner: NilString(f.User.Name, "root"),
			Group: NilString(f.Group.Name, "root"),
			MTime: uint32(packTime),
			Type:  rpmpack.GenericFile,
		}
		r.AddFile(rpmfile)
	}

	//TODO: I've never tested links, don't trust it !
	for _, l := range config.Storage.Links {
		glog.Infof("LINK: %s %s\n", l.Path, l.Node.Path)
		l.Path = RelocateForRpmOstree(l.Path)
		rpmfile := rpmpack.RPMFile{
			Name:  l.Node.Path,
			Body:  []byte(l.Path),
			Mode:  0755,
			Owner: NilString(l.User.Name, "root"),
			Group: NilString(l.Group.Name, "root"),
			MTime: uint32(packTime),
			Type:  rpmpack.GenericFile,
		}
		rpmfile.Mode |= 00120000
		r.AddFile(rpmfile)

	}
	//Loop through the units, put them in the right spot
	for _, u := range config.Systemd.Units {
		unitFile := filepath.Join("/", SystemdUnitsPath(), u.Name)

		glog.Infof("UNIT: %s %s %t\n", u.Name, unitFile, NilBool(u.Enabled, true))
		rpmfile := rpmpack.RPMFile{
			Name:  unitFile,
			Body:  []byte(NilString(u.Contents, "")),
			Mode:  0644,
			Owner: "root",
			Group: "root",
			MTime: uint32(packTime),
			Type:  rpmpack.GenericFile,
		}
		r.AddFile(rpmfile)

		//Some of these units may have dropins
		for _, dropin := range u.Dropins {
			dropinFile := filepath.Join("/", SystemdDropinsPath(u.Name), dropin.Name)
			glog.Infof("\tDROPIN: %s %s\n", dropin.Name, dropinFile)

		}

	}

	return nil

}

func SystemdUnitsPath() string {
	return filepath.Join("etc", "systemd", "system")
}

func SystemdRuntimeUnitsPath() string {
	return filepath.Join("run", "systemd", "system")
}

func SystemdRuntimeUnitWantsPath(unitName string) string {
	return filepath.Join("run", "systemd", "system", unitName+".wants")
}

func SystemdDropinsPath(unitName string) string {
	return filepath.Join("etc", "systemd", "system", unitName+".d")
}

func SystemdRuntimeDropinsPath(unitName string) string {
	return filepath.Join("run", "systemd", "system", unitName+".d")
}
