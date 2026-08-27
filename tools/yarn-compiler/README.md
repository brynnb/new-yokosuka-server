# New Yokosuka Yarn compiler bridge

This small process is the boundary between the Go server and the official Yarn
Spinner compiler. It accepts one JSON compile request on standard input and
returns one JSON result containing exact diagnostics, compiled protobuf bytes,
the string table, and node metadata.

It deliberately does not implement or repair Yarn syntax. `YarnSpinner.Compiler`
is pinned by the project and package lock. New Yokosuka's Go command registry is
sent with each request so function type-checking uses the same definitions as
the editor and runtime.

Build with a .NET 9 SDK:

```sh
dotnet publish tools/yarn-compiler/NewYokosuka.YarnCompiler.csproj -c Release
```
