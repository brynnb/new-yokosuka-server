using System.Text.Json;
using Google.Protobuf;
using Yarn.Compiler;

namespace NewYokosuka.YarnCompiler;

internal static class CompilerHost
{
    internal const string ProtocolVersion = "new-yokosuka-yarn-compiler-v1";
    internal const string CompilerVersion = "yarnspinner-3.2.1";
    internal static readonly JsonSerializerOptions JsonOptions = new()
    {
        PropertyNamingPolicy = JsonNamingPolicy.CamelCase,
        PropertyNameCaseInsensitive = true,
    };

    internal static async Task Run()
    {
        try
        {
            var request = await JsonSerializer.DeserializeAsync<CompileRequest>(Console.OpenStandardInput(), JsonOptions)
                ?? throw new InvalidDataException("A compile request is required.");
            if (string.IsNullOrWhiteSpace(request.FileName) || string.IsNullOrWhiteSpace(request.Source))
            {
                throw new InvalidDataException("fileName and source are required.");
            }

            var job = CompilationJob.CreateFromString(request.FileName, request.Source, new Yarn.Library(), 3);
            job.Declarations = FunctionDeclarations.Create(request.Functions);
            var result = Yarn.Compiler.Compiler.Compile(job);
            var diagnostics = result.Diagnostics.Select(diagnostic => new CompilerDiagnostic(
                diagnostic.Code ?? string.Empty,
                diagnostic.Severity.ToString().ToLowerInvariant(),
                diagnostic.Message,
                diagnostic.FileName ?? request.FileName,
                diagnostic.Range.Start.Line,
                diagnostic.Range.Start.Character,
                diagnostic.Range.End.Line,
                diagnostic.Range.End.Character
            )).ToArray();
            var lines = result.StringTable!
                .OrderBy(entry => entry.Key, StringComparer.Ordinal)
                .Select(entry => new CompiledLine(
                    entry.Key,
                    entry.Value.text,
                    entry.Value.fileName,
                    entry.Value.nodeName,
                    entry.Value.lineNumber,
                    entry.Value.isImplicitTag,
                    entry.Value.metadata,
                    entry.Value.shadowLineID
                )).ToArray();
            var nodes = (result.NodeMetadata ?? [])
                .OrderBy(node => node.Title, StringComparer.Ordinal)
                .Select(node => new CompiledNode(
                    node.Title,
                    node.SourceTitle,
                    node.UniqueTitle,
                    node.NodeGroup,
                    node.FunctionCalls.Order(StringComparer.Ordinal).ToArray(),
                    node.CommandCalls.Order(StringComparer.Ordinal).ToArray(),
                    node.VariableReferences.Order(StringComparer.Ordinal).ToArray(),
                    node.CharacterNames.Order(StringComparer.Ordinal).ToArray(),
                    node.Tags.Order(StringComparer.Ordinal).ToArray(),
                    node.OptionCount,
                    node.HeaderStartLine,
                    node.TitleLine,
                    node.BodyStartLine,
                    node.BodyEndLine
                )).ToArray();
            var calls = CallExtractor.Extract(result.ParseResults);
            var response = new CompileResponse(
                ProtocolVersion,
                CompilerVersion,
                !result.ContainsErrors && result.Program is not null,
                result.Program is null ? null : Convert.ToBase64String(result.Program.ToByteArray()),
                diagnostics,
                lines,
                nodes,
                calls
            );
            await JsonSerializer.SerializeAsync(Console.OpenStandardOutput(), response, JsonOptions);
        }
        catch (Exception exception)
        {
            Console.Error.WriteLine(exception);
            Environment.ExitCode = 1;
        }
    }
}
