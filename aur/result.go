package aur

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/jguer/yay/pacman"
	"github.com/jguer/yay/util"
)

// Result describes an AUR package.
type Result struct {
	Conflicts      []string `json:"Conflicts"`
	Depends        []string `json:"Depends"`
	Description    string   `json:"Description"`
	FirstSubmitted int      `json:"FirstSubmitted"`
	ID             int      `json:"ID"`
	Keywords       []string `json:"Keywords"`
	LastModified   int64    `json:"LastModified"`
	License        []string `json:"License"`
	Maintainer     string   `json:"Maintainer"`
	MakeDepends    []string `json:"MakeDepends"`
	Name           string   `json:"Name"`
	NumVotes       int      `json:"NumVotes"`
	OptDepends     []string `json:"OptDepends"`
	OutOfDate      int      `json:"OutOfDate"`
	PackageBase    string   `json:"PackageBase"`
	PackageBaseID  int      `json:"PackageBaseID"`
	Provides       []string `json:"Provides"`
	URL            string   `json:"URL"`
	URLPath        string   `json:"URLPath"`
	Version        string   `json:"Version"`
	Installed      bool
	Popularity     float32 `json:"Popularity"`
}

// Dependencies returns package dependencies not installed belonging to AUR
// 0 is Repo, 1 is Foreign.
func (a *Result) Dependencies() (runDeps [2][]string, makeDeps [2][]string, err error) {
	var q Query
	if len(a.Depends) == 0 && len(a.MakeDepends) == 0 {
		var n int
		q, n, err = Info(a.Name)
		if n == 0 || err != nil {
			err = fmt.Errorf("Unable to search dependencies, %s", err)
			return
		}
	} else {
		q = append(q, *a)
	}

	depSearch := pacman.BuildDependencies(a.Depends)
	if len(a.Depends) != 0 {
		runDeps[0], runDeps[1] = depSearch(q[0].Depends, true, false)
		if len(runDeps[0]) != 0 || len(runDeps[1]) != 0 {
			fmt.Println("\x1b[1;32m=>\x1b[1;33m Run Dependencies: \x1b[0m")
			printDeps(runDeps[0], runDeps[1])
		}
	}

	if len(a.MakeDepends) != 0 {
		makeDeps[0], makeDeps[1] = depSearch(q[0].MakeDepends, false, false)
		if len(makeDeps[0]) != 0 || len(makeDeps[1]) != 0 {
			fmt.Println("\x1b[1;32m=>\x1b[1;33m Make Dependencies: \x1b[0m")
			printDeps(makeDeps[0], makeDeps[1])
		}
	}
	depSearch(a.MakeDepends, false, true)

	err = nil
	return
}

func printDeps(repoDeps []string, aurDeps []string) {
	if len(repoDeps) != 0 {
		fmt.Print("\x1b[1;32m==> Repository dependencies: \x1b[0m")
		for _, repoD := range repoDeps {
			fmt.Print("\x1b[33m", repoD, " \x1b[0m")
		}
		fmt.Print("\n")

	}
	if len(aurDeps) != 0 {
		fmt.Print("\x1b[1;32m==> AUR dependencies: \x1b[0m")
		for _, aurD := range aurDeps {
			fmt.Print("\x1b[33m", aurD, " \x1b[0m")
		}
		fmt.Print("\n")
	}
}

// Install handles install from Info Result.
func (a *Result) Install(flags []string) (finalmdeps []string, err error) {
	fmt.Printf("\x1b[1;32m==> Installing\x1b[33m %s\x1b[0m\n", a.Name)
	if a.Maintainer == "" {
		fmt.Println("\x1b[1;31;40m==> Warning:\x1b[0;;40m This package is orphaned.\x1b[0m")
	}
	dir := util.BaseDir + a.PackageBase + "/"

	if _, err = os.Stat(dir); os.IsNotExist(err) {
		if err = util.DownloadAndUnpack(BaseURL+a.URLPath, util.BaseDir, false); err != nil {
			return
		}
	} else {
		if !util.ContinueTask("Directory exists. Clean Build?", "yY") {
			os.RemoveAll(util.BaseDir + a.PackageBase)
			if err = util.DownloadAndUnpack(BaseURL+a.URLPath, util.BaseDir, false); err != nil {
				return
			}
		}
	}

	if !util.ContinueTask("Edit PKGBUILD?", "yY") {
		editcmd := exec.Command(util.Editor(), dir+"PKGBUILD")
		editcmd.Stdin, editcmd.Stdout, editcmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		editcmd.Run()
	}

	runDeps, makeDeps, err := a.Dependencies()
	if err != nil {
		return
	}

	repoDeps := append(runDeps[0], makeDeps[0]...)
	aurDeps := append(runDeps[1], makeDeps[1]...)
	finalmdeps = append(finalmdeps, makeDeps[0]...)
	finalmdeps = append(finalmdeps, makeDeps[1]...)

	if len(aurDeps) != 0 || len(repoDeps) != 0 {
		if !util.ContinueTask("Continue?", "nN") {
			return finalmdeps, fmt.Errorf("user did not like the dependencies")
		}
	}

	aurQ, n, _ := MultiInfo(aurDeps)
	if n != len(aurDeps) {
		aurQ.MissingPackage(aurDeps)
		if !util.ContinueTask("Continue?", "nN") {
			return finalmdeps, fmt.Errorf("unable to install dependencies")
		}
	}

	var depArgs []string
	if util.NoConfirm {
		depArgs = []string{"--asdeps", "--noconfirm"}
	} else {
		depArgs = []string{"--asdeps"}
	}
	// Repo dependencies
	if len(repoDeps) != 0 {
		errR := pacman.Install(repoDeps, depArgs)
		if errR != nil {
			return finalmdeps, errR
		}
	}

	// Handle AUR dependencies
	for _, dep := range aurQ {
		finalmdepsR, errA := dep.Install(depArgs)
		finalmdeps = append(finalmdeps, finalmdepsR...)

		if errA != nil {
			pacman.CleanRemove(repoDeps)
			pacman.CleanRemove(aurDeps)
			return finalmdeps, errA
		}
	}

	err = os.Chdir(dir)
	if err != nil {
		return
	}

	args := []string{"-sri"}
	args = append(args, flags...)
	makepkgcmd := exec.Command(util.MakepkgBin, args...)
	makepkgcmd.Stdin, makepkgcmd.Stdout, makepkgcmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	err = makepkgcmd.Run()
	return
}

// PrintInfo prints package info like pacman -Si.
func (a *Result) PrintInfo() {
	fmt.Println("\x1b[1;37mRepository      :\x1b[0m", "aur")
	fmt.Println("\x1b[1;37mName            :\x1b[0m", a.Name)
	fmt.Println("\x1b[1;37mVersion         :\x1b[0m", a.Version)
	fmt.Println("\x1b[1;37mDescription     :\x1b[0m", a.Description)
	if a.URL != "" {
		fmt.Println("\x1b[1;37mURL             :\x1b[0m", a.URL)
	} else {
		fmt.Println("\x1b[1;37mURL             :\x1b[0m", "None")
	}
	fmt.Println("\x1b[1;37mLicenses        :\x1b[0m", a.License)

	if len(a.Provides) != 0 {
		fmt.Println("\x1b[1;37mProvides        :\x1b[0m", a.Provides)
	} else {
		fmt.Println("\x1b[1;37mProvides        :\x1b[0m", "None")
	}

	if len(a.Depends) != 0 {
		fmt.Println("\x1b[1;37mDepends On      :\x1b[0m", a.Depends)
	} else {
		fmt.Println("\x1b[1;37mDepends On      :\x1b[0m", "None")
	}

	if len(a.MakeDepends) != 0 {
		fmt.Println("\x1b[1;37mMake depends On :\x1b[0m", a.MakeDepends)
	} else {
		fmt.Println("\x1b[1;37mMake depends On :\x1b[0m", "None")
	}

	if len(a.OptDepends) != 0 {
		fmt.Println("\x1b[1;37mOptional Deps   :\x1b[0m", a.OptDepends)
	} else {
		fmt.Println("\x1b[1;37mOptional Deps   :\x1b[0m", "None")
	}

	if len(a.Conflicts) != 0 {
		fmt.Println("\x1b[1;37mConflicts With  :\x1b[0m", a.Conflicts)
	} else {
		fmt.Println("\x1b[1;37mConflicts With  :\x1b[0m", "None")
	}

	if a.Maintainer != "" {
		fmt.Println("\x1b[1;37mMaintainer      :\x1b[0m", a.Maintainer)
	} else {
		fmt.Println("\x1b[1;37mMaintainer      :\x1b[0m", "None")
	}
	fmt.Println("\x1b[1;37mVotes           :\x1b[0m", a.NumVotes)
	fmt.Println("\x1b[1;37mPopularity      :\x1b[0m", a.Popularity)

	if a.OutOfDate != 0 {
		fmt.Println("\x1b[1;37mOut-of-date     :\x1b[0m", "Yes")
	}

}

// RemoveMakeDeps receives a make dependency list and removes those
// that are no longer necessary.
func RemoveMakeDeps(depS []string) (err error) {
	hanging := pacman.SliceHangingPackages(depS)

	if len(hanging) != 0 {
		if !util.ContinueTask("Confirm Removal?", "nN") {
			return nil
		}
		err = pacman.CleanRemove(hanging)
	}

	return
}
