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

type Versions struct {
	Code int
	Name string
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
	bumpTypes := []string{"major", "minor", "patch", "none"}
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

func getVersionsFromFile(file string) (Versions, error) {
	bytes, err := ioutil.ReadFile(file)
	if err != nil {
		return Versions{}, err
	}
	re := regexp.MustCompile(`versionName\s+"([0-9.]+)"`)
	matchesName := re.FindStringSubmatch(string(bytes))

	if len(matchesName) != 2 {
		return Versions{}, errors.New("Failed to match `versionName`")
	}

	re = regexp.MustCompile(`versionCode\s+(\d+)`)
	matchesCode := re.FindStringSubmatch(string(bytes))

	if len(matchesCode) != 2 {
		return Versions{}, errors.New("Failed to match `versionCode`")
	}

	versionCode, err := strconv.ParseInt(matchesCode[1], 10, 32)
	if err != nil {
		return Versions{}, err
	}

	return Versions{
		Name: matchesName[1],
		Code: int(versionCode),
	}, nil
}

func bumpVersions(bumpType string, versions Versions) (Versions, error) {
	versionName, err := semver.NewVersion(versions.Name)
	if err != nil {
		return Versions{}, err
	}

	switch bumpType {
	case "major":
		versionName.BumpMajor()
	case "minor":
		versionName.BumpMinor()
	case "patch":
		versionName.BumpPatch()
	default:
	}

	return Versions{
		Name: versionName.String(),
		Code: versions.Code + 1,
	}, nil
}

func setVersionsToFile(file string, versions Versions) error {
	bytes, err := ioutil.ReadFile(file)
	if err != nil {
		return err
	}

	re := regexp.MustCompile(`versionName\s+"([0-9.]+)"`)
	body := re.ReplaceAllString(string(bytes), "versionName \"" + versions.Name + "\"")

	re = regexp.MustCompile(`versionCode\s+(\d+)`)
	body = re.ReplaceAllString(body, "versionCode " + strconv.Itoa(versions.Code))

	ioutil.WriteFile(file, []byte(body), 0644)

	return nil
}

func exportEnvironmentWithEnvman(key, value string) error {
	cmd := command.New("envman", "add", "--key", key)
	cmd.SetStdin(strings.NewReader(value))
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

		versions, err := getVersionsFromFile(buildGradleFile)
		if err != nil {
			log.Fail("Failed to get versions: %s", err)
		}
		log.Detail("versionCode: %d", versions.Code)
		log.Detail("versionName: %s", versions.Name)

		newVersions, err := bumpVersions(configs.BumpType, versions)
		if err != nil {
			log.Fail("Failed to bump versions: %s", err)
		}


		log.Info("New versions:")
		log.Detail("versionCode: %d", newVersions.Code)
		log.Detail("versionName: %s", newVersions.Name)

		if err := exportEnvironmentWithEnvman("BUMP_VERSION_CODE", strconv.Itoa(newVersions.Code)); err != nil {
			log.Fail("Failed to export enviroment (BUMP_VERSION_CODE): %s", err)
		}
		if err := exportEnvironmentWithEnvman("BUMP_VERSION_NAME", newVersions.Name); err != nil {
			log.Fail("Failed to export enviroment (BUMP_VERSION_CODE): %s", err)
		}

		if err := setVersionsToFile(buildGradleFile, newVersions); err != nil {
			log.Fail("Failed to export enviroment (BUMP_VERSION_CODE): %s", err)
		}

		log.Info("Git diff:")
		if err := gitCommand("diff", buildGradleFile); err != nil {
			log.Fail("Failed to git diff: %s", err)
		}

		if err := gitCommand("add", buildGradleFile); err != nil {
			log.Fail("Failed to git diff: %s", err)
		}

		if err := gitCommand("commit", "-m", "Bump version to " + newVersions.Name); err != nil {
			log.Fail("Failed to git diff: %s", err)
		}

		if err := gitCommand("push", "origin", "HEAD"); err != nil {
			log.Fail("Failed to git diff: %s", err)
		}

		if err := gitCommand("checkout", "master"); err != nil {
			log.Fail("Failed to git diff: %s", err)
		}

		if err := gitCommand("merge", "develop"); err != nil {
			log.Fail("Failed to git diff: %s", err)
		}

		if err := gitCommand("tag", "-a", newVersions.Name, "-m", newVersions.Name); err != nil {
			log.Fail("Failed to git diff: %s", err)
		}

		if err := gitCommand("push", "origin", "HEAD", "--follow-tags"); err != nil {
			log.Fail("Failed to git diff: %s", err)
		}
	}
}
