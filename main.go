package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/bitrise-io/go-utils/command"
	"github.com/bitrise-io/go-utils/sliceutil"
	log "github.com/thefuntasty/bitrise-step-bump-android/logger"
	"io/ioutil"
	"regexp"
	"github.com/coreos/go-semver/semver"
	"strconv"
)

type ConfigsModel struct {
	BumpType string
}

func createConfigsModelFromEnvs() ConfigsModel {
	return ConfigsModel{
		BumpType: os.Getenv("bump_type"),
	}
}

func (configs ConfigsModel) print() {
	log.Info("Configs:")
	log.Detail("- BumpType: %s", configs.BumpType)
}

func (configs ConfigsModel) validate() (string, error) {
	bumpTypes := []string{"major", "minor", "patch"}
	if !sliceutil.IsStringInSlice(configs.BumpType, bumpTypes) {
		return "", errors.New("Invalid bump type!")
	}

	return "", nil
}

func find(dir, nameInclude string) ([]string, error) {
	cmdSlice := []string{"grep"}
	cmdSlice = append(cmdSlice, "-l")
	cmdSlice = append(cmdSlice, "-r", "versionCode")
	cmdSlice = append(cmdSlice, "--include", nameInclude)
	cmdSlice = append(cmdSlice, dir)

	log.Detail(command.PrintableCommandArgs(false, cmdSlice))

	out, err := command.New(cmdSlice[0], cmdSlice[1:]...).RunAndReturnTrimmedOutput()
	if err != nil {
		return []string{}, err
	}

	split := strings.Split(out, "\n")
	files := []string{}
	for _, item := range split {
		trimmed := strings.TrimSpace(item)
		if trimmed != "" {
			files = append(files, trimmed)
		}
	}

	return files, nil
}

func getVersionsFromFile(file string) (string, string, error) {
	bytes, err := ioutil.ReadFile(file)
	if err != nil {
		return "", "", err
	}
	re := regexp.MustCompile(`versionName\s+"([0-9.]+)"`)
	matchesName := re.FindStringSubmatch(string(bytes))

	if len(matchesName) != 2 {
		return "", "", errors.New("Failed to match `versionName`")
	}

	re = regexp.MustCompile(`versionCode\s+(\d+)`)
	matchesCode := re.FindStringSubmatch(string(bytes))

	if len(matchesCode) != 2 {
		return "", "", errors.New("Failed to match `versionCode`")
	}

	return matchesCode[1], matchesName[1], nil
}

func setVersionsToFile(file string, versionCode int, versionName string) error {
	bytes, err := ioutil.ReadFile(file)
	if err != nil {
		return err
	}

	re := regexp.MustCompile(`versionName\s+"([0-9.]+)"`)
	body := re.ReplaceAllString(string(bytes), "versionName \"" + versionName + "\"")

	re = regexp.MustCompile(`versionCode\s+(\d+)`)
	body = re.ReplaceAllString(body, "versionCode " + strconv.Itoa(versionCode))

	ioutil.WriteFile(file, []byte(body), 0644)

	return nil
}

func exportEnvironmentWithEnvman(keyStr, valueStr string) error {
	cmd := command.New("envman", "add", "--key", keyStr)
	cmd.SetStdin(strings.NewReader(valueStr))
	return cmd.Run()
}

func gitCommand(args ...string) error {
	cmd := command.New("git", args...)
	cmd.SetStdout(os.Stdout)
	cmd.SetStderr(os.Stderr)
	return cmd.Run()
}

func main() {
	configs := createConfigsModelFromEnvs()
	configs.print()
	if explanation, err := configs.validate(); err != nil {
		fmt.Println()
		log.Error("Issue with input: %s", err)
		fmt.Println()

		if explanation != "" {
			fmt.Println(explanation)
			fmt.Println()
		}

		os.Exit(1)
	}

	log.Info("Find build.gradle file...")
	buildGradleFiles, err := find(".", "build.gradle")
	if err != nil {
		log.Fail("Failed to find `build.gradle` file: %s", err)
	}

	if len(buildGradleFiles) == 0 {
		log.Fail("No `build.gradle` file found")
	}

	if len(buildGradleFiles) != 1 {
		log.Fail("Found more than one `build.gradle` file")
	}

	for _, buildGradleFile := range buildGradleFiles {
		log.Info("Current versions:")

		versionCode, versionName, err := getVersionsFromFile(buildGradleFile)
		versionCodeInt, err := strconv.ParseInt(versionCode, 10, 32)
		if err != nil {
			log.Fail("Failed to get `versionName` or `versionCode`: %s", err)
		}
		log.Detail("versionCode: %d", versionCodeInt)
		log.Detail("versionName: %s", versionName)

		version, err := semver.NewVersion(versionName)
		if err != nil {
			log.Fail("Failed to parse `versionName`: %s", err)
		}

		switch configs.BumpType {
		case "major":
			version.BumpMajor()
		case "minor":
			version.BumpMinor()
		case "patch":
			version.BumpPatch()
		}

		log.Info("New versions:")
		newVersionCode := int(versionCodeInt + 1)
		log.Detail("versionCode: %d", newVersionCode)
		log.Detail("versionName: %s", version.String())

		if err := exportEnvironmentWithEnvman("BUMP_VERSION_CODE", strconv.Itoa(int(newVersionCode))); err != nil {
			log.Fail("Failed to export enviroment (BUMP_VERSION_CODE): %s", err)
		}
		if err := exportEnvironmentWithEnvman("BUMP_VERSION_NAME", version.String()); err != nil {
			log.Fail("Failed to export enviroment (BUMP_VERSION_CODE): %s", err)
		}

		if err := setVersionsToFile(buildGradleFile, newVersionCode, version.String()); err != nil {
			log.Fail("Failed to export enviroment (BUMP_VERSION_CODE): %s", err)
		}

		log.Info("Git diff:")
		gitCommand("diff", buildGradleFile)

		gitCommand("add", buildGradleFile)

		gitCommand("commit", "-m", "Bump version to " + version.String())

		gitCommand("push", "origin", "HEAD")

		gitCommand("checkout", "master")

		gitCommand("merge", "develop")

		gitCommand("push", "origin", "HEAD")
	}
}
