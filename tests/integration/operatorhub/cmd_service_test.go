package integration

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/odo/tests/helper"
)

var _ = Describe("odo service command tests for OperatorHub", func() {

	var project string

	BeforeEach(func() {
		SetDefaultEventuallyTimeout(10 * time.Minute)
		SetDefaultConsistentlyDuration(30 * time.Second)
		// TODO: remove this when OperatorHub integration is fully baked into odo
		// helper.CmdShouldPass("odo", "preference", "set", "Experimental", "true")
	})

	preSetup := func() {
		project = helper.CreateRandProject()
		helper.CmdShouldPass("odo", "project", "set", project)

		// wait till oc can see the all operators installed by setup script in the namespace
		odoArgs := []string{"catalog", "list", "services"}
		operators := []string{"etcdoperator", "service-binding-operator"}
		for _, operator := range operators {
			helper.WaitForCmdOut("odo", odoArgs, 5, true, func(output string) bool {
				return strings.Contains(output, operator)
			})
		}
	}

	cleanPreSetup := func() {
		helper.DeleteProject(project)
	}

	Context("When Operators are installed in the cluster", func() {

		JustBeforeEach(func() {
			preSetup()
		})

		JustAfterEach(func() {
			cleanPreSetup()
		})

		It("should list operators installed in the namespace", func() {
			stdOut := helper.CmdShouldPass("odo", "catalog", "list", "services")
			helper.MatchAllInOutput(stdOut, []string{"Services available through Operators", "etcdoperator"})
		})

		It("should not allow interactive mode command to be executed", func() {
			stdOut := helper.CmdShouldFail("odo", "service", "create")
			Expect(stdOut).To(ContainSubstring("please use a valid command to start an Operator backed service"))
		})
	})

	Context("When creating and deleting an operator backed service", func() {

		JustBeforeEach(func() {
			preSetup()
		})

		JustAfterEach(func() {
			cleanPreSetup()
		})

		It("should be able to create, list and then delete EtcdCluster from its alm example", func() {
			operators := helper.CmdShouldPass("odo", "catalog", "list", "services")
			etcdOperator := regexp.MustCompile(`etcdoperator\.*[a-z][0-9]\.[0-9]\.[0-9]-clusterwide`).FindString(operators)
			stdOut := helper.CmdShouldPass("odo", "service", "create", fmt.Sprintf("%s/EtcdCluster", etcdOperator), "--project", project)
			Expect(stdOut).To(ContainSubstring("Service 'example' was created"))

			// now verify if the pods for the operator have started
			pods := helper.CmdShouldPass("oc", "get", "pods", "-n", project)
			// Look for pod with example name because that's the name etcd will give to the pods.
			etcdPod := regexp.MustCompile(`example-.[a-z0-9]*`).FindString(pods)

			ocArgs := []string{"get", "pods", etcdPod, "-o", "template=\"{{.status.phase}}\"", "-n", project}
			helper.WaitForCmdOut("oc", ocArgs, 1, true, func(output string) bool {
				return strings.Contains(output, "Running")
			})

			// now test listing of the service using odo
			stdOut = helper.CmdShouldPass("odo", "service", "list")
			Expect(stdOut).To(ContainSubstring("EtcdCluster/example"))

			// now test the deletion of the service using odo
			helper.CmdShouldPass("odo", "service", "delete", "EtcdCluster/example", "-f")

			// now try deleting the same service again. It should fail with error message
			stdOut = helper.CmdShouldFail("odo", "service", "delete", "EtcdCluster/example", "-f")
			Expect(stdOut).To(ContainSubstring("Couldn't find service named"))
		})

		It("should be able to create service with name passed on CLI", func() {
			name := helper.RandString(6)
			svcFullName := strings.Join([]string{"EtcdCluster", name}, "/")
			operators := helper.CmdShouldPass("odo", "catalog", "list", "services")
			etcdOperator := regexp.MustCompile(`etcdoperator\.*[a-z][0-9]\.[0-9]\.[0-9]-clusterwide`).FindString(operators)
			helper.CmdShouldPass("odo", "service", "create", fmt.Sprintf("%s/EtcdCluster", etcdOperator), name, "--project", project)

			// now verify if the pods for the operator have started
			pods := helper.CmdShouldPass("oc", "get", "pods", "-n", project)
			// Look for pod with custom name because that's the name etcd will give to the pods.
			compileString := name + `-.[a-z0-9]*`
			etcdPod := regexp.MustCompile(compileString).FindString(pods)

			ocArgs := []string{"get", "pods", etcdPod, "-o", "template=\"{{.status.phase}}\"", "-n", project}
			helper.WaitForCmdOut("oc", ocArgs, 1, true, func(output string) bool {
				return strings.Contains(output, "Running")
			})

			// now try creating service with same name again. it should fail
			stdOut := helper.CmdShouldFail("odo", "service", "create", fmt.Sprintf("%s/EtcdCluster", etcdOperator), name, "--project", project)
			Expect(stdOut).To(ContainSubstring(fmt.Sprintf("service %q already exists", svcFullName)))

			helper.CmdShouldPass("odo", "service", "delete", svcFullName, "-f")
		})
	})

	Context("When deleting an invalid operator backed service", func() {
		It("should correctly detect invalid service names", func() {
			names := []string{"EtcdCluster", "EtcdCluster/", "/example"}

			for _, name := range names {
				stdOut := helper.CmdShouldFail("odo", "service", "delete", name, "-f")
				Expect(stdOut).To(ContainSubstring("couldn't split %q into exactly two", name))
			}
		})
	})

	Context("When using dry-run option to create operator backed service", func() {

		JustBeforeEach(func() {
			preSetup()
		})

		JustAfterEach(func() {
			cleanPreSetup()
		})

		It("should only output the definition of the CR that will be used to start service", func() {
			// First let's grab the etcd operator's name from "odo catalog list services" output
			operators := helper.CmdShouldPass("odo", "catalog", "list", "services")
			etcdOperator := regexp.MustCompile(`etcdoperator\.*[a-z][0-9]\.[0-9]\.[0-9]-clusterwide`).FindString(operators)

			stdOut := helper.CmdShouldPass("odo", "service", "create", fmt.Sprintf("%s/EtcdCluster", etcdOperator), "--dry-run", "--project", project)
			helper.MatchAllInOutput(stdOut, []string{"apiVersion", "kind"})
		})
	})

	Context("Should be able to search from catalog", func() {

		JustBeforeEach(func() {
			preSetup()
		})

		JustAfterEach(func() {
			cleanPreSetup()
		})

		It("should only output the definition of the CR that will be used to start service", func() {
			stdOut := helper.CmdShouldPass("odo", "catalog", "search", "service", "etcd")
			helper.MatchAllInOutput(stdOut, []string{"etcdoperator", "EtcdCluster"})

			stdOut = helper.CmdShouldPass("odo", "catalog", "search", "service", "EtcdCluster")
			helper.MatchAllInOutput(stdOut, []string{"etcdoperator", "EtcdCluster"})

			stdOut = helper.CmdShouldFail("odo", "catalog", "search", "service", "dummy")
			Expect(stdOut).To(ContainSubstring("no service matched the query: dummy"))
		})
	})

	Context("When using from-file option", func() {

		JustBeforeEach(func() {
			preSetup()
		})

		JustAfterEach(func() {
			cleanPreSetup()
		})

		It("should be able to create a service", func() {
			// First let's grab the etcd operator's name from "odo catalog list services" output
			operators := helper.CmdShouldPass("odo", "catalog", "list", "services")
			etcdOperator := regexp.MustCompile(`etcdoperator\.*[a-z][0-9]\.[0-9]\.[0-9]-clusterwide`).FindString(operators)

			stdOut := helper.CmdShouldPass("odo", "service", "create", fmt.Sprintf("%s/EtcdCluster", etcdOperator), "--dry-run", "--project", project)

			// stdOut contains the yaml specification. Store it to a file
			randomFileName := helper.RandString(6) + ".yaml"
			fileName := filepath.Join(os.TempDir(), randomFileName)
			if err := ioutil.WriteFile(fileName, []byte(stdOut), 0644); err != nil {
				fmt.Printf("Could not write yaml spec to file %s because of the error %v", fileName, err.Error())
			}

			// now create operator backed service
			helper.CmdShouldPass("odo", "service", "create", "--from-file", fileName, "--project", project)

			// now verify if the pods for the operator have started
			pods := helper.CmdShouldPass("oc", "get", "pods", "-n", project)
			// Look for pod with example name because that's the name etcd will give to the pods.
			etcdPod := regexp.MustCompile(`example-.[a-z0-9]*`).FindString(pods)

			ocArgs := []string{"get", "pods", etcdPod, "-o", "template=\"{{.status.phase}}\"", "-n", project}
			helper.WaitForCmdOut("oc", ocArgs, 1, true, func(output string) bool {
				return strings.Contains(output, "Running")
			})

			helper.CmdShouldPass("odo", "service", "delete", "EtcdCluster/example", "-f")
		})

		It("should be able to create a service with name passed on CLI", func() {
			name := helper.RandString(6)
			// First let's grab the etcd operator's name from "odo catalog list services" output
			operators := helper.CmdShouldPass("odo", "catalog", "list", "services")
			etcdOperator := regexp.MustCompile(`etcdoperator\.*[a-z][0-9]\.[0-9]\.[0-9]-clusterwide`).FindString(operators)

			stdOut := helper.CmdShouldPass("odo", "service", "create", fmt.Sprintf("%s/EtcdCluster", etcdOperator), "--dry-run", "--project", project)

			// stdOut contains the yaml specification. Store it to a file
			randomFileName := helper.RandString(6) + ".yaml"
			fileName := filepath.Join(os.TempDir(), randomFileName)
			if err := ioutil.WriteFile(fileName, []byte(stdOut), 0644); err != nil {
				fmt.Printf("Could not write yaml spec to file %s because of the error %v", fileName, err.Error())
			}

			// now create operator backed service
			helper.CmdShouldPass("odo", "service", "create", "--from-file", fileName, name, "--project", project)

			// Attempting to create service with same name should fail
			stdOut = helper.CmdShouldFail("odo", "service", "create", "--from-file", fileName, name, "--project", project)
			Expect(stdOut).To(ContainSubstring("please provide a different name or delete the existing service first"))
		})
	})

	Context("When using from-file option", func() {

		JustBeforeEach(func() {
			preSetup()
		})

		JustAfterEach(func() {
			cleanPreSetup()
		})

		It("should fail to create service if metadata doesn't exist or is invalid", func() {
			noMetadata := `
apiVersion: etcd.database.coreos.com/v1beta2
kind: EtcdCluster
spec:
  size: 3
  version: 3.2.13
`

			invalidMetadata := `
apiVersion: etcd.database.coreos.com/v1beta2
kind: EtcdCluster
metadata:
  noname: noname
spec:
  size: 3
  version: 3.2.13
`

			noMetaFile := helper.RandString(6) + ".yaml"
			fileName := filepath.Join("/tmp", noMetaFile)
			if err := ioutil.WriteFile(fileName, []byte(noMetadata), 0644); err != nil {
				fmt.Printf("Could not write yaml spec to file %s because of the error %v", fileName, err.Error())
			}

			// now create operator backed service
			stdOut := helper.CmdShouldFail("odo", "service", "create", "--from-file", fileName, "--project", project)
			Expect(stdOut).To(ContainSubstring("couldn't find \"metadata\" in the yaml"))

			invalidMetaFile := helper.RandString(6) + ".yaml"
			fileName = filepath.Join("/tmp", invalidMetaFile)
			if err := ioutil.WriteFile(fileName, []byte(invalidMetadata), 0644); err != nil {
				fmt.Printf("Could not write yaml spec to file %s because of the error %v", fileName, err.Error())
			}

			// now create operator backed service
			stdOut = helper.CmdShouldFail("odo", "service", "create", "--from-file", fileName, "--project", project)
			Expect(stdOut).To(ContainSubstring("couldn't find metadata.name in the yaml"))

		})
	})

	Context("JSON output", func() {

		JustBeforeEach(func() {
			preSetup()
		})

		JustAfterEach(func() {
			cleanPreSetup()
		})

		It("listing catalog of services", func() {
			jsonOut := helper.CmdShouldPass("odo", "catalog", "list", "services", "-o", "json")
			helper.MatchAllInOutput(jsonOut, []string{"etcdoperator"})
		})
	})

	Context("When operator backed services are created", func() {

		JustBeforeEach(func() {
			preSetup()
		})

		JustAfterEach(func() {
			cleanPreSetup()
		})

		It("should list the services if they exist", func() {
			operators := helper.CmdShouldPass("odo", "catalog", "list", "services")
			etcdOperator := regexp.MustCompile(`etcdoperator\.*[a-z][0-9]\.[0-9]\.[0-9]-clusterwide`).FindString(operators)
			helper.CmdShouldPass("odo", "service", "create", fmt.Sprintf("%s/EtcdCluster", etcdOperator), "--project", project)

			// now verify if the pods for the operator have started
			pods := helper.CmdShouldPass("oc", "get", "pods", "-n", project)
			// Look for pod with example name because that's the name etcd will give to the pods.
			etcdPod := regexp.MustCompile(`example-.[a-z0-9]*`).FindString(pods)

			ocArgs := []string{"get", "pods", etcdPod, "-o", "template=\"{{.status.phase}}\"", "-n", project}
			helper.WaitForCmdOut("oc", ocArgs, 1, true, func(output string) bool {
				return strings.Contains(output, "Running")
			})

			stdOut := helper.CmdShouldPass("odo", "service", "list")
			helper.MatchAllInOutput(stdOut, []string{"example", "EtcdCluster"})

			// now check for json output
			jsonOut := helper.CmdShouldPass("odo", "service", "list", "-o", "json")
			helper.MatchAllInOutput(jsonOut, []string{"\"apiVersion\": \"etcd.database.coreos.com/v1beta2\"", "\"kind\": \"EtcdCluster\"", "\"name\": \"example\""})

			helper.CmdShouldPass("odo", "service", "delete", "EtcdCluster/example", "-f")

			// Now let's check the output again to ensure expected behaviour
			stdOut = helper.CmdShouldFail("odo", "service", "list")
			jsonOut = helper.CmdShouldFail("odo", "service", "list", "-o", "json")

			msg := fmt.Sprintf("no operator backed services found in namespace: %s", project)
			msgWithQuote := fmt.Sprintf("\"message\": \"no operator backed services found in namespace: %s\"", project)
			Expect(stdOut).To(ContainSubstring(msg))
			helper.MatchAllInOutput(jsonOut, []string{msg, msgWithQuote})
		})
	})

	Context("When linking devfile component with Operator backed service", func() {
		var context, currentWorkingDirectory, devfilePath string
		const devfile = "devfile.yaml"

		JustBeforeEach(func() {
			preSetup()
			context = helper.CreateNewContext()
			devfilePath = filepath.Join(context, devfile)
			currentWorkingDirectory = helper.Getwd()
			helper.Chdir(context)
			helper.CopyExampleDevFile(filepath.Join("source", "devfiles", "nodejs", devfile), devfilePath)
		})

		JustAfterEach(func() {
			cleanPreSetup()
			helper.Chdir(currentWorkingDirectory)
			helper.DeleteDir(context)
		})

		It("should fail if service name doesn't adhere to <service-type>/<service-name> format", func() {
			if os.Getenv("KUBERNETES") == "true" {
				Skip("This is a OpenShift specific scenario, skipping")
			}

			componentName := helper.RandString(6)
			helper.CmdShouldPass("odo", "create", componentName)

			stdOut := helper.CmdShouldFail("odo", "link", "EtcdCluster")
			Expect(stdOut).To(ContainSubstring("Invalid service name"))

			stdOut = helper.CmdShouldFail("odo", "link", "EtcdCluster/")
			Expect(stdOut).To(ContainSubstring("Invalid service name"))

			stdOut = helper.CmdShouldFail("odo", "link", "/example")
			Expect(stdOut).To(ContainSubstring("Invalid service name"))
		})

		It("should fail if the provided service doesn't exist in the namespace", func() {
			if os.Getenv("KUBERNETES") == "true" {
				Skip("This is a OpenShift specific scenario, skipping")
			}

			componentName := helper.RandString(6)
			helper.CmdShouldPass("odo", "create", componentName)
			helper.CmdShouldPass("odo", "push")

			stdOut := helper.CmdShouldFail("odo", "link", "EtcdCluster/example")
			Expect(stdOut).To(ContainSubstring("Couldn't find service named %q", "EtcdCluster/example"))
		})

		It("should successfully connect and disconnect a component with an existing service", func() {
			if os.Getenv("KUBERNETES") == "true" {
				Skip("This is a OpenShift specific scenario, skipping")
			}

			componentName := helper.RandString(6)
			helper.CmdShouldPass("odo", "create", componentName, "--devfile", "devfile.yaml", "--starter")
			helper.CmdShouldPass("odo", "push")

			// start the Operator backed service first
			operators := helper.CmdShouldPass("odo", "catalog", "list", "services")
			etcdOperator := regexp.MustCompile(`etcdoperator\.*[a-z][0-9]\.[0-9]\.[0-9]-clusterwide`).FindString(operators)
			helper.CmdShouldPass("odo", "service", "create", fmt.Sprintf("%s/EtcdCluster", etcdOperator), "--project", project)

			// now verify if the pods for the operator have started
			pods := helper.CmdShouldPass("oc", "get", "pods", "-n", project)
			// Look for pod with example name because that's the name etcd will give to the pods.
			etcdPod := regexp.MustCompile(`example-.[a-z0-9]*`).FindString(pods)

			ocArgs := []string{"get", "pods", etcdPod, "-o", "template=\"{{.status.phase}}\"", "-n", project}
			helper.WaitForCmdOut("oc", ocArgs, 1, true, func(output string) bool {
				return strings.Contains(output, "Running")
			})

			stdOut := helper.CmdShouldPass("odo", "link", "EtcdCluster/example")
			Expect(stdOut).To(ContainSubstring("Successfully created link between component"))
			helper.CmdShouldPass("odo", "push")
			stdOut = helper.CmdShouldFail("odo", "link", "EtcdCluster/example")
			Expect(stdOut).To(ContainSubstring("already linked with the service"))

			stdOut = helper.CmdShouldPass("odo", "unlink", "EtcdCluster/example")
			Expect(stdOut).To(ContainSubstring("Successfully unlinked component"))
			helper.CmdShouldPass("odo", "push")

			// verify that sbr is deleted
			stdOut = helper.CmdShouldFail("odo", "unlink", "EtcdCluster/example")
			Expect(stdOut).To(ContainSubstring("failed to unlink the service"))
		})

		It("should fail if we delete a link outside of odo (using oc)", func() {
			if os.Getenv("KUBERNETES") == "true" {
				Skip("This is a OpenShift specific scenario, skipping")
			}

			componentName := helper.RandString(6)
			helper.CmdShouldPass("odo", "create", componentName)
			helper.CmdShouldPass("odo", "push")

			// start the Operator backed service first
			operators := helper.CmdShouldPass("odo", "catalog", "list", "services")
			etcdOperator := regexp.MustCompile(`etcdoperator\.*[a-z][0-9]\.[0-9]\.[0-9]-clusterwide`).FindString(operators)
			helper.CmdShouldPass("odo", "service", "create", fmt.Sprintf("%s/EtcdCluster", etcdOperator), "--project", project)

			// now verify if the pods for the operator have started
			pods := helper.CmdShouldPass("oc", "get", "pods", "-n", project)
			// Look for pod with example name because that's the name etcd will give to the pods.
			etcdPod := regexp.MustCompile(`example-.[a-z0-9]*`).FindString(pods)

			ocArgs := []string{"get", "pods", etcdPod, "-o", "template=\"{{.status.phase}}\"", "-n", project}
			helper.WaitForCmdOut("oc", ocArgs, 1, true, func(output string) bool {
				return strings.Contains(output, "Running")
			})

			stdOut := helper.CmdShouldPass("odo", "link", "EtcdCluster/example")
			Expect(stdOut).To(ContainSubstring("Successfully created link between component"))
			helper.CmdShouldPass("odo", "push")

			sbrName := strings.Join([]string{componentName, "etcdcluster", "example"}, "-")
			helper.CmdShouldPass("oc", "delete", fmt.Sprintf("ServiceBinding/%s", sbrName))
			stdOut = helper.CmdShouldFail("odo", "unlink", "EtcdCluster/example")
			helper.MatchAllInOutput(stdOut, []string{"component's link with", "has been deleted outside odo"})
		})
	})
})
