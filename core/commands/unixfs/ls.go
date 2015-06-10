package unixfs

import (
	"bytes"
	"fmt"
	"io"
	"text/tabwriter"

	cmds "github.com/ipfs/go-ipfs/commands"
	core "github.com/ipfs/go-ipfs/core"
	path "github.com/ipfs/go-ipfs/path"
	unixfs "github.com/ipfs/go-ipfs/unixfs"
	unixfspb "github.com/ipfs/go-ipfs/unixfs/pb"
)

type LsLink struct {
	Name, Hash string
	Size       uint64
	Type       unixfspb.Data_DataType
}

type LsObject struct {
	Argument string
	Links    []LsLink
}

type LsOutput struct {
	Objects []*LsObject
}

var LsCmd = &cmds.Command{
	Helptext: cmds.HelpText{
		Tagline: "List directory contents for Unix-filesystem objects",
		ShortDescription: `
Retrieves the object named by <ipfs-or-ipns-path> and displays the
contents with the following format:

  <hash> <type> <size> <name>

For files, the child size is the total size of the file contents.  For
directories, the child size is the IPFS link size.
`,
	},

	Arguments: []cmds.Argument{
		cmds.StringArg("ipfs-path", true, true, "The path to the IPFS object(s) to list links from").EnableStdin(),
	},
	Options: []cmds.Option{
		cmds.BoolOption("headers", "", "Print table headers (Hash, Size, Name)"),
	},
	Run: func(req cmds.Request, res cmds.Response) {
		node, err := req.Context().GetNode()
		if err != nil {
			res.SetError(err, cmds.ErrNormal)
			return
		}

		// get options early -> exit early in case of error
		if _, _, err := req.Option("headers").Bool(); err != nil {
			res.SetError(err, cmds.ErrNormal)
			return
		}

		paths := req.Arguments()

		output := make([]*LsObject, len(paths))
		for i, fpath := range paths {
			ctx := req.Context().Context
			merkleNode, err := core.Resolve(ctx, node, path.Path(fpath))
			if err != nil {
				res.SetError(err, cmds.ErrNormal)
				return
			}

			unixFSNode, err := unixfs.FromBytes(merkleNode.Data)
			if err != nil {
				res.SetError(err, cmds.ErrNormal)
				return
			}

			output[i] = &LsObject{Argument: paths[i]}

			t := unixFSNode.GetType()
			switch t {
			default:
				res.SetError(fmt.Errorf("unrecognized type: %s", t), cmds.ErrImplementation)
				return
			case unixfspb.Data_File:
				key, err := merkleNode.Key()
				if err != nil {
					res.SetError(err, cmds.ErrNormal)
					return
				}
				output[i].Links = []LsLink{LsLink{
					Name: paths[i],
					Hash: key.String(),
					Type: t,
					Size: unixFSNode.GetFilesize(),
				}}
			case unixfspb.Data_Directory:
				output[i].Links = make([]LsLink, len(merkleNode.Links))
				for j, link := range merkleNode.Links {
					link.Node, err = link.GetNode(ctx, node.DAG)
					if err != nil {
						res.SetError(err, cmds.ErrNormal)
						return
					}
					d, err := unixfs.FromBytes(link.Node.Data)
					if err != nil {
						res.SetError(err, cmds.ErrNormal)
						return
					}
					lsLink := LsLink{
						Name: link.Name,
						Hash: link.Hash.B58String(),
						Type: d.GetType(),
					}
					if lsLink.Type == unixfspb.Data_File {
						lsLink.Size = d.GetFilesize()
					} else {
						lsLink.Size = link.Size
					}
					output[i].Links[j] = lsLink
				}
			}
		}

		res.SetOutput(&LsOutput{Objects: output})
	},
	Marshalers: cmds.MarshalerMap{
		cmds.Text: func(res cmds.Response) (io.Reader, error) {

			headers, _, _ := res.Request().Option("headers").Bool()
			output := res.Output().(*LsOutput)
			buf := new(bytes.Buffer)
			w := tabwriter.NewWriter(buf, 1, 2, 1, ' ', 0)
			lastObjectDirHeader := false
			for i, object := range output.Objects {
				singleObject := (len(object.Links) == 1 &&
					object.Links[0].Name == object.Argument)
				if len(output.Objects) > 1 && !singleObject {
					if i > 0 {
						fmt.Fprintln(w)
					}
					fmt.Fprintf(w, "%s:\n", object.Argument)
					lastObjectDirHeader = true
				} else {
					if lastObjectDirHeader {
						fmt.Fprintln(w)
					}
					lastObjectDirHeader = false
				}
				if headers {
					fmt.Fprintln(w, "Hash\tType\tSize\tName")
				}
				for _, link := range object.Links {
					fmt.Fprintf(w, "%s\t%s\t%v\t%s\n",
						link.Hash, link.Type.String(), link.Size, link.Name)
				}
			}
			w.Flush()

			return buf, nil
		},
	},
	Type: LsOutput{},
}
