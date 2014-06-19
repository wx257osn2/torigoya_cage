//
// Copyright yutopp 2014 - .
//
// Distributed under the Boost Software License, Version 1.0.
// (See accompanying file LICENSE_1_0.txt or copy at
// http://www.boost.org/LICENSE_1_0.txt)
//

package torigoya

import (
	"errors"
	"fmt"
	"os"
	"io/ioutil"
	"path/filepath"

	"gopkg.in/v1/yaml"
	"github.com/mattn/go-shellwords"
)

// ==================================================
type SelectableCommand struct {
	Default		[]string
	Select		[]string `yaml:"select,flow"`
}

func (sc *SelectableCommand) IsEmpty() bool { return sc.Default == nil || sc.Select == nil }


type PhaseDetail struct {
	File					string
	Extension				string
	Command					string
	Env						map[string]string
	AllowedCommandLine		map[string]SelectableCommand `yaml:"allowed_command_line"`
	FixedCommandLine		[][]string `yaml:"fixed_command_line"`
}

func (pd *PhaseDetail) MakeCompleteArgs(
	command_line string,
	selected_options [][]string,
) ([]string, error) {
	for _, v := range selected_options {
		if err := pd.isValidOption(v); err != nil {
			return nil, err
		}
	}

	args := []string{}

	// command
	if len(pd.Command) == 0 {
		return nil, errors.New("command can not be empty")
	}
	args = append(args, pd.Command)

	// selected user commands(structured)
	for _, v := range selected_options {
		args = append(args, v...)
	}

	// fixed commands
	for _, v := range pd.FixedCommandLine {
		args = append(args, v...)
	}

	// user command
	u_args, err := shellwords.Parse(command_line)
	if err != nil {
		return nil, err
	}
	args = append(args, u_args...)

	return args, nil
}

func (pd *PhaseDetail) isValidOption(selected_option []string) error {
	if !( len(selected_option) == 1 || len(selected_option) == 2 ) {
		return errors.New(fmt.Sprintf("length of the option should be 1 or 2 (but %d)", len(selected_option)))
	}

	if val,ok := pd.AllowedCommandLine[selected_option[0]]; ok {
		if len(selected_option) == 2 {
			// TODO: fix to bin search
			for _, v := range pd.AllowedCommandLine[selected_option[0]].Select {
				if v == selected_option[1] {
					return nil
				}
			}
			return errors.New(fmt.Sprintf("value(%s) was not found in key(%s)", selected_option[1], selected_option[0]))

		} else {
			// selected option is only key
			if val.IsEmpty() {
				return nil

			} else {
				return errors.New("nil value can not be selected")
			}
		}

	} else {
		return errors.New(fmt.Sprintf("key(%s) was not found", selected_option[0]))
	}
}


type ProcProfile struct {
	Version						string
	IsBuildRequired				bool `yaml:"is_build_required"`
	IsLinkIndependent			bool `yaml:"is_link_independent"`

	Source, Compile, Link, Run	PhaseDetail
}


// ==================================================
type ProcIndex struct {
	Id			int
	Name		string
	Runnable	bool
	Path		string
}

type ProcIndexList []ProcIndex


// ==================================================
type ProcConfigTable map[int]ProcConfigUnit		// proc_id:config_unit
type ProcConfigUnit struct {
	Index		ProcIndex
	Versioned	map[string]ProcProfile
}


// ==================================================
// ==================================================
func makeProcProfileFromBuf(buffer []byte) (ProcProfile, error) {
	profile := ProcProfile{}

	if err := yaml.Unmarshal(buffer, &profile); err != nil {
		return profile, err
	}

	return profile, nil
}

func makeProcProfileFromPath(filepath string) (ProcProfile, error) {
	b, err := ioutil.ReadFile(filepath)
	if err != nil {
		return ProcProfile{}, err
	}

	return makeProcProfileFromBuf(b)
}


func makeProcIndexListFromBuf(buffer []byte) (ProcIndexList, error) {
	var index_list ProcIndexList

	if err := yaml.Unmarshal(buffer, &index_list); err != nil {
		return nil, err
	}

	return index_list, nil
}

func makeProcIndexListFromPath(filepath string) (ProcIndexList, error) {
	b, err := ioutil.ReadFile(filepath)
	if err != nil {
		return nil, err
	}

	return makeProcIndexListFromBuf(b)
}


func globProfiles(proc_path string) (map[string]ProcProfile, error) {
	result := make(map[string]ProcProfile)

	if err := filepath.Walk(proc_path, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		proc_profile, err := makeProcProfileFromPath(path)
		if err != nil {
			return err;
		}

		result[proc_profile.Version] = proc_profile

		return nil
	}); err != nil {
		return nil, err
	}

	return result, nil
}


func LoadProcConfigs(base_path string) (ProcConfigTable, error) {
	var result = make(ProcConfigTable)

	index_list, err := makeProcIndexListFromPath(filepath.Join(base_path, "languages.yml"))
	if err != nil {
		return nil, err
	}

	for _, proc_index := range index_list {
		versioned_proc_profiles, err := globProfiles(filepath.Join(base_path, proc_index.Path))
		if err != nil {
			return nil, err
		}

		result[proc_index.Id] = ProcConfigUnit{
			proc_index,
			versioned_proc_profiles,
		}
	}

	return result, nil
}
