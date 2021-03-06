package cmds

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"golang.org/x/sync/errgroup"

	"github.com/pachyderm/pachyderm/src/client"
	pfsclient "github.com/pachyderm/pachyderm/src/client/pfs"
	"github.com/pachyderm/pachyderm/src/server/pfs/fuse"
	"github.com/pachyderm/pachyderm/src/server/pfs/pretty"
	"github.com/pachyderm/pachyderm/src/server/pkg/cmd"

	"github.com/spf13/cobra"
	"go.pedge.io/pkg/cobra"
)

// Cmds returns a slice containing pfs commands.
func Cmds(address string) []*cobra.Command {
	var fileNumber int
	var fileModulus int
	var blockNumber int
	var blockModulus int
	shard := func() *pfsclient.Shard {
		return &pfsclient.Shard{
			FileNumber:   uint64(fileNumber),
			FileModulus:  uint64(fileModulus),
			BlockNumber:  uint64(blockNumber),
			BlockModulus: uint64(blockModulus),
		}
	}

	addShardFlags := func(cmd *cobra.Command) {
		cmd.Flags().IntVarP(&fileNumber, "file-shard", "s", 0, "file shard to read")
		cmd.Flags().IntVarP(&fileModulus, "file-modulus", "m", 1, "modulus of file shard")
		cmd.Flags().IntVarP(&blockNumber, "block-shard", "b", 0, "block shard to read")
		cmd.Flags().IntVarP(&blockModulus, "block-modulus", "n", 1, "modulus of block shard")
	}

	repo := &cobra.Command{
		Use:   "repo",
		Short: "Docs for repos.",
		Long: `Repos, short for repository, are the top level data object in Pachyderm.

Repos are created with create-repo.`,
		Run: cmd.RunFixedArgs(0, func(args []string) error {
			return nil
		}),
	}

	createRepo := &cobra.Command{
		Use:   "create-repo repo-name",
		Short: "Create a new repo.",
		Long:  "Create a new repo.",
		Run: cmd.RunFixedArgs(1, func(args []string) error {
			client, err := client.NewFromAddress(address)
			if err != nil {
				return err
			}
			return client.CreateRepo(args[0])
		}),
	}

	inspectRepo := &cobra.Command{
		Use:   "inspect-repo repo-name",
		Short: "Return info about a repo.",
		Long:  "Return info about a repo.",
		Run: cmd.RunFixedArgs(1, func(args []string) error {
			client, err := client.NewFromAddress(address)
			if err != nil {
				return err
			}
			repoInfo, err := client.InspectRepo(args[0])
			if err != nil {
				return err
			}
			if repoInfo == nil {
				return fmt.Errorf("repo %s not found", args[0])
			}
			return pretty.PrintDetailedRepoInfo(repoInfo)
		}),
	}

	var listRepoProvenance cmd.RepeatedStringArg
	listRepo := &cobra.Command{
		Use:   "list-repo",
		Short: "Return all repos.",
		Long:  "Reutrn all repos.",
		Run: cmd.RunFixedArgs(0, func(args []string) error {
			c, err := client.NewFromAddress(address)
			if err != nil {
				return err
			}
			repoInfos, err := c.ListRepo(listRepoProvenance)
			if err != nil {
				return err
			}
			writer := tabwriter.NewWriter(os.Stdout, 20, 1, 3, ' ', 0)
			pretty.PrintRepoHeader(writer)
			for _, repoInfo := range repoInfos {
				pretty.PrintRepoInfo(writer, repoInfo)
			}
			return writer.Flush()
		}),
	}
	listRepo.Flags().VarP(&listRepoProvenance, "provenance", "p", "list only repos with the specified repos provenance")

	var force bool
	deleteRepo := &cobra.Command{
		Use:   "delete-repo repo-name",
		Short: "Delete a repo.",
		Long:  "Delete a repo.",
		Run: cmd.RunFixedArgs(1, func(args []string) error {
			client, err := client.NewFromAddress(address)
			if err != nil {
				return err
			}
			return client.DeleteRepo(args[0], force)
		}),
	}
	deleteRepo.Flags().BoolVarP(&force, "force", "f", false, "remove the repo regardless of errors; use with care")

	commit := &cobra.Command{
		Use:   "commit",
		Short: "Docs for commits.",
		Long: `Commits are atomic transactions on the content of a repo.

Creating a commit is a multistep process:
- start a new commit with start-commit
- write files to it through fuse or with put-file
- finish the new commit with finish-commit

Commits that have been started but not finished are NOT durable storage.
Commits become reliable (and immutable) when they are finished.

Commits can be created with another commit as a parent.
This layers the data in the commit over the data in the parent.`,
		Run: cmd.RunFixedArgs(0, func(args []string) error {
			return nil
		}),
	}

	var parentCommitID string
	startCommit := &cobra.Command{
		Use:   "start-commit repo-name [branch]",
		Short: "Start a new commit.",
		Long:  "Start a new commit with parent-commit-id as the parent.",
		Run: cmd.RunBoundedArgs(1, 2, func(args []string) error {
			client, err := client.NewFromAddress(address)
			if err != nil {
				return err
			}
			branch := ""
			if len(args) == 2 {
				branch = args[1]
			}
			commit, err := client.StartCommit(args[0],
				parentCommitID, branch)
			if err != nil {
				return err
			}
			fmt.Println(commit.ID)
			return nil
		}),
	}
	startCommit.Flags().StringVarP(&parentCommitID, "parent", "p", "", "parent id")

	var cancel bool
	finishCommit := &cobra.Command{
		Use:   "finish-commit repo-name commit-id",
		Short: "Finish a started commit.",
		Long:  "Finish a started commit. Commit-id must be a writeable commit.",
		Run: cmd.RunFixedArgs(2, func(args []string) error {
			client, err := client.NewFromAddress(address)
			if err != nil {
				return err
			}
			if cancel {
				return client.CancelCommit(args[0], args[1])
			}
			return client.FinishCommit(args[0], args[1])
		}),
	}
	finishCommit.Flags().BoolVarP(&cancel, "cancel", "c", false, "cancel the commit")

	inspectCommit := &cobra.Command{
		Use:   "inspect-commit repo-name commit-id",
		Short: "Return info about a commit.",
		Long:  "Return info about a commit.",
		Run: cmd.RunFixedArgs(2, func(args []string) error {
			client, err := client.NewFromAddress(address)
			if err != nil {
				return err
			}
			commitInfo, err := client.InspectCommit(args[0], args[1])
			if err != nil {
				return err
			}
			if commitInfo == nil {
				return fmt.Errorf("commit %s not found", args[1])
			}
			return pretty.PrintDetailedCommitInfo(commitInfo)
		}),
	}

	var all bool
	var block bool
	var listCommitProvenance cmd.RepeatedStringArg
	listCommit := &cobra.Command{
		Use:   "list-commit repo-name",
		Short: "Return all commits on a set of repos",
		Long: `Return all commits on a set of repos.

Examples:

	# return commits in repo "foo" and repo "bar"
	$ pachctl list-commit foo bar

	# return commits in repo "foo" since commit abc123 and those in repo "bar" since commit def456
	$ pachctl list-commit foo/abc123 bar/def456

	# return commits in repo "foo" that have commits
	# "bar/abc123" and "baz/def456" as provenance
	$ pachctl list-commit foo -p bar/abc123 -p baz/def456

`,
		Run: pkgcobra.Run(func(args []string) error {
			commits, err := cmd.ParseCommits(args)
			if err != nil {
				return err
			}

			var repos []string
			var fromCommits []string
			for _, commit := range commits {
				repos = append(repos, commit.Repo.Name)
				fromCommits = append(fromCommits, commit.ID)
			}

			c, err := client.NewFromAddress(address)
			if err != nil {
				return err
			}

			provenance, err := cmd.ParseCommits(listCommitProvenance)
			if err != nil {
				return err
			}
			status := pfsclient.CommitStatus_NORMAL
			if all {
				status = pfsclient.CommitStatus_ALL
			}
			commitInfos, err := c.ListCommit(repos, fromCommits, client.CommitTypeNone, block, status, provenance)
			if err != nil {
				return err
			}

			writer := tabwriter.NewWriter(os.Stdout, 20, 1, 3, ' ', 0)
			pretty.PrintCommitInfoHeader(writer)
			for _, commitInfo := range commitInfos {
				pretty.PrintCommitInfo(writer, commitInfo)
			}
			return writer.Flush()
		}),
	}
	listCommit.Flags().BoolVarP(&all, "all", "a", false, "list all commits including cancelled commits")
	listCommit.Flags().BoolVarP(&block, "block", "b", false, "block until there are new commits since the from commits")
	listCommit.Flags().VarP(&listCommitProvenance, "provenance", "p",
		"list only commits with the specified `commit`s provenance, commits are specified as RepoName/CommitID")

	var repos cmd.RepeatedStringArg
	flushCommit := &cobra.Command{
		Use:   "flush-commit commit [commit ...]",
		Short: "Wait for all commits caused by the specified commits to finish and return them.",
		Long: `Wait for all commits caused by the specified commits to finish and return them.

Examples:

	# return commits caused by foo/abc123 and bar/def456
	$ pachctl flush-commit foo/abc123 bar/def456

	# return commits caused by foo/abc123 leading to repos bar and baz
	$ pachctl flush-commit foo/abc123 -r bar -r baz

`,
		Run: pkgcobra.Run(func(args []string) error {
			commits, err := cmd.ParseCommits(args)
			if err != nil {
				return err
			}

			c, err := client.NewFromAddress(address)
			if err != nil {
				return err
			}

			var toRepos []*pfsclient.Repo
			for _, repoName := range repos {
				toRepos = append(toRepos, client.NewRepo(repoName))
			}

			commitInfos, err := c.FlushCommit(commits, toRepos)
			if err != nil {
				return err
			}

			writer := tabwriter.NewWriter(os.Stdout, 20, 1, 3, ' ', 0)
			pretty.PrintCommitInfoHeader(writer)
			for _, commitInfo := range commitInfos {
				pretty.PrintCommitInfo(writer, commitInfo)
			}
			return writer.Flush()
		}),
	}
	flushCommit.Flags().VarP(&repos, "repos", "r", "Wait only for commits leading to a specific set of repos")

	listBranch := &cobra.Command{
		Use:   "list-branch repo-name",
		Short: "Return all branches on a repo.",
		Long:  "Return all branches on a repo.",
		Run: cmd.RunFixedArgs(1, func(args []string) error {
			client, err := client.NewFromAddress(address)
			if err != nil {
				return err
			}
			commitInfos, err := client.ListBranch(args[0])
			if err != nil {
				return err
			}
			writer := tabwriter.NewWriter(os.Stdout, 20, 1, 3, ' ', 0)
			pretty.PrintCommitInfoHeader(writer)
			for _, commitInfo := range commitInfos {
				pretty.PrintCommitInfo(writer, commitInfo)
			}
			return writer.Flush()
		}),
	}

	file := &cobra.Command{
		Use:   "file",
		Short: "Docs for files.",
		Long: `Files are the lowest level data object in Pachyderm.

Files can be written to started (but not finished) commits with put-file.
Files can be read from finished commits with get-file.`,
		Run: cmd.RunFixedArgs(0, func(args []string) error {
			return nil
		}),
	}

	var filePaths []string
	var recursive bool
	var commitFlag bool
	var inputFile string
	// putFilePath is a helper for putFile
	putFilePath := func(client *client.APIClient, args []string, filePath string) error {
		if filePath == "-" {
			if len(args) < 3 {
				return errors.New("either a path or the -f flag needs to be provided")
			}
			_, err := client.PutFile(args[0], args[1], args[2], os.Stdin)
			return err
		}
		// try parsing the filename as a url, if it is one do a PutFileURL
		if url, err := url.Parse(filePath); err == nil && url.Scheme != "" {
			if len(args) < 3 {
				return client.PutFileURL(args[0], args[1], url.Path, url.String())
			}
			return client.PutFileURL(args[0], args[1], args[2], url.String())
		}
		if !recursive {
			if len(args) == 3 {
				return cpFile(client, args[0], args[1], args[2], filePath)
			}
			return cpFile(client, args[0], args[1], filePath, filePath)
		}
		var eg errgroup.Group
		filepath.Walk(filePath, func(path string, info os.FileInfo, err error) error {
			if info.IsDir() {
				return nil
			}
			if len(args) == 3 {
				eg.Go(func() error { return cpFile(client, args[0], args[1], filepath.Join(args[2], path), path) })
			}
			eg.Go(func() error { return cpFile(client, args[0], args[1], path, path) })
			return nil
		})
		return eg.Wait()
	}
	putFile := &cobra.Command{
		Use:   "put-file repo-name commit-id path/to/file/in/pfs",
		Short: "Put a file into the filesystem.",
		Long: `Put-file supports a number of ways to insert data into pfs:
Put data from stdin as repo/commit/path:
	echo "data" | pachctl put-file repo commit path

Start a new commmit on branch, put data from stdin as repo/branch/path and
finish the commit:
	echo "data" | pachctl put-file -c repo branch path

Put a file from the local filesystem as repo/commit/path:
	pachctl put-file repo commit path -f file

Put a file from the local filesystem as repo/commit/file:
	pachctl put-file repo commit -f file

Put the contents of a directory as repo/commit/path/dir/file:
	pachctl put-file -r repo commit path -f dir

Put the contents of a directory as repo/commit/dir/file:
	pachctl put-file -r repo commit -f dir

Put the data from a URL as repo/commit/path:
	pachctl put-file repo commit path -f http://host/url_path

Put the data from a URL as repo/commit/url_path:
	pachctl put-file repo commit -f http://host/url_path

Put several files or URLs that are listed in file.
Files and URLs should be newline delimited.
	pachctl put-file repo commit -i file
`,
		Run: cmd.RunBoundedArgs(2, 3, func(args []string) (retErr error) {
			client, err := client.NewFromAddress(address)
			if err != nil {
				return err
			}
			if commitFlag {
				commit, err := client.StartCommit(args[0],
					"", args[1])
				if err != nil {
					return err
				}
				defer func() {
					if retErr != nil {
						// something errored so we try to cancel the commit
						if err := client.CancelCommit(commit.Repo.Name, commit.ID); err != nil {
							fmt.Printf("Error cancelling commit: %s", err.Error())
						}
					} else {
						if err := client.FinishCommit(commit.Repo.Name, commit.ID); err != nil && retErr == nil {
							retErr = err
						}
					}
				}()
			}
			var eg errgroup.Group
			if inputFile != "" {
				var r io.Reader
				if inputFile == "-" {
					r = os.Stdin
				} else {
					inputFile, err := os.Open(inputFile)
					if err != nil {
						return err
					}
					r = inputFile
				}
				// scan line by line
				scanner := bufio.NewScanner(r)
				for scanner.Scan() {
					eg.Go(func() error { return putFilePath(client, args, scanner.Text()) })
				}
			}
			for _, filePath := range filePaths {
				eg.Go(func() error { return putFilePath(client, args, filePath) })
			}
			return eg.Wait()
		}),
	}
	putFile.Flags().StringSliceVarP(&filePaths, "file", "f", []string{"-"}, "The file to be put, it can be a local file or a URL.")
	putFile.Flags().StringVarP(&inputFile, "input-file", "i", "", "Read filepaths or URLs from a file.  If - is used, paths are read from the standard input.")
	putFile.Flags().BoolVarP(&recursive, "recursive", "r", false, "Recursively put the files in a directory.")
	putFile.Flags().BoolVarP(&commitFlag, "commit", "c", false, "Start and finish the commit in addition to putting data.")

	var fromCommitID string
	var fullFile bool
	var unsafe bool
	addFileFlags := func(cmd *cobra.Command) {
		cmd.Flags().StringVarP(&fromCommitID, "from", "f", "", "only consider data written since this commit")
		cmd.Flags().BoolVar(&fullFile, "full-file", false, "if there has been data since the from commit return the full file")
		cmd.Flags().BoolVar(&unsafe, "unsafe", false, "use this flag if you need to read data written in the current commit; this operation will race with concurrent writes")
	}
	getFile := &cobra.Command{
		Use:   "get-file repo-name commit-id path/to/file",
		Short: "Return the contents of a file.",
		Long:  "Return the contents of a file.",
		Run: cmd.RunFixedArgs(3, func(args []string) error {
			client, err := client.NewFromAddress(address)
			if err != nil {
				return err
			}
			return client.GetFile(args[0], args[1], args[2], 0, 0, fromCommitID, fullFile, shard(), os.Stdout)
		}),
	}
	addShardFlags(getFile)
	addFileFlags(getFile)

	inspectFile := &cobra.Command{
		Use:   "inspect-file repo-name commit-id path/to/file",
		Short: "Return info about a file.",
		Long:  "Return info about a file.",
		Run: cmd.RunFixedArgs(3, func(args []string) error {
			client, err := client.NewFromAddress(address)
			if err != nil {
				return err
			}
			fileInfo, err := client.InspectFile(args[0], args[1], args[2], fromCommitID, fullFile, shard())
			if err != nil {
				return err
			}
			if fileInfo == nil {
				return fmt.Errorf("file %s not found", args[2])
			}
			return pretty.PrintDetailedFileInfo(fileInfo)
		}),
	}
	addShardFlags(inspectFile)
	addFileFlags(inspectFile)

	listFile := &cobra.Command{
		Use:   "list-file repo-name commit-id path/to/dir",
		Short: "Return the files in a directory.",
		Long:  "Return the files in a directory.",
		Run: cmd.RunBoundedArgs(2, 3, func(args []string) error {
			client, err := client.NewFromAddress(address)
			if err != nil {
				return err
			}
			var path string
			if len(args) == 3 {
				path = args[2]
			}
			fileInfos, err := client.ListFile(args[0], args[1], path, fromCommitID, fullFile, shard(), true)
			if err != nil {
				return err
			}
			writer := tabwriter.NewWriter(os.Stdout, 20, 1, 3, ' ', 0)
			pretty.PrintFileInfoHeader(writer)
			for _, fileInfo := range fileInfos {
				pretty.PrintFileInfo(writer, fileInfo)
			}
			return writer.Flush()
		}),
	}
	addShardFlags(listFile)
	addFileFlags(listFile)

	deleteFile := &cobra.Command{
		Use:   "delete-file repo-name commit-id path/to/file",
		Short: "Delete a file.",
		Long:  "Delete a file.",
		Run: cmd.RunFixedArgs(2, func(args []string) error {
			client, err := client.NewFromAddress(address)
			if err != nil {
				return err
			}
			return client.DeleteFile(args[0], args[1], args[2], false, "")
		}),
	}

	var debug bool
	mount := &cobra.Command{
		Use:   "mount path/to/mount/point",
		Short: "Mount pfs locally.",
		Long:  "Mount pfs locally.",
		Run: cmd.RunFixedArgs(1, func(args []string) error {
			client, err := client.NewFromAddress(address)
			if err != nil {
				return err
			}
			mounter := fuse.NewMounter(address, client.PfsAPIClient)
			mountPoint := args[0]
			err = mounter.Mount(mountPoint, shard(), nil, nil, debug)
			if err != nil {
				return err
			}
			return nil
		}),
	}
	addShardFlags(mount)
	mount.Flags().BoolVarP(&debug, "debug", "d", false, "turn on debug messages")

	var result []*cobra.Command
	result = append(result, repo)
	result = append(result, createRepo)
	result = append(result, inspectRepo)
	result = append(result, listRepo)
	result = append(result, deleteRepo)
	result = append(result, commit)
	result = append(result, startCommit)
	result = append(result, finishCommit)
	result = append(result, inspectCommit)
	result = append(result, listCommit)
	result = append(result, flushCommit)
	result = append(result, listBranch)
	result = append(result, file)
	result = append(result, putFile)
	result = append(result, getFile)
	result = append(result, inspectFile)
	result = append(result, listFile)
	result = append(result, deleteFile)
	result = append(result, mount)
	return result
}

func parseCommitMounts(args []string) []*fuse.CommitMount {
	var result []*fuse.CommitMount
	for _, arg := range args {
		commitMount := &fuse.CommitMount{Commit: client.NewCommit("", "")}
		repo, commitAlias := path.Split(arg)
		commitMount.Commit.Repo.Name = path.Clean(repo)
		split := strings.Split(commitAlias, ":")
		if len(split) > 0 {
			commitMount.Commit.ID = split[0]
		}
		if len(split) > 1 {
			commitMount.Alias = split[1]
		}
		result = append(result, commitMount)
	}
	return result
}

func cpFile(client *client.APIClient, repo string, commit string, path string, filePath string) (retErr error) {
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer func() {
		if err := f.Close(); err != nil && retErr == nil {
			retErr = err
		}
	}()
	_, err = client.PutFile(repo, commit, path, f)
	return err
}
