namespace NewYokosuka.YarnCompiler;

internal sealed record CompileRequest(
    string FileName,
    string Source,
    IReadOnlyList<FunctionDefinition>? Functions
);

internal sealed record FunctionDefinition(
    string Name,
    string ReturnType,
    IReadOnlyList<string>? ParameterTypes
);

internal sealed record CompileResponse(
    string ProtocolVersion,
    string CompilerVersion,
    bool Valid,
    string? ProgramBase64,
    IReadOnlyList<CompilerDiagnostic> Diagnostics,
    IReadOnlyList<CompiledLine> Lines,
    IReadOnlyList<CompiledNode> Nodes,
    IReadOnlyList<CompiledCall> Calls
);

internal sealed record CompilerDiagnostic(
    string Code,
    string Severity,
    string Message,
    string FileName,
    int StartLine,
    int StartColumn,
    int EndLine,
    int EndColumn
);

internal sealed record CompiledLine(
    string Id,
    string? Text,
    string FileName,
    string NodeName,
    int LineNumber,
    bool HasImplicitId,
    IReadOnlyList<string> Metadata,
    string? ShadowLineId
);

internal sealed record CompiledNode(
    string Title,
    string? SourceTitle,
    string? UniqueTitle,
    string? Group,
    IReadOnlyList<string> FunctionCalls,
    IReadOnlyList<string> CommandCalls,
    IReadOnlyList<string> VariableReferences,
    IReadOnlyList<string> CharacterNames,
    IReadOnlyList<string> Tags,
    int OptionCount,
    int HeaderStartLine,
    int TitleLine,
    int BodyStartLine,
    int BodyEndLine
);

internal sealed record CompiledCall(
    string Kind,
    string Name,
    string Node,
    string FileName,
    int StartLine,
    int StartColumn,
    int EndLine,
    int EndColumn,
    string? ParseError,
    IReadOnlyList<CompiledArgument> Arguments
);

internal sealed record CompiledArgument(
    string Type,
    bool IsStatic,
    string? Value
);

internal sealed record RuntimeStartRequest(
    string ProtocolVersion,
    string ProgramBase64,
    string StartNode,
    IReadOnlyList<FunctionDefinition>? Functions,
    IReadOnlyList<RuntimeVariable>? Variables
);

internal sealed record RuntimeVariable(string Name, RuntimeValue Value);

internal sealed record RuntimeValue(string Type, string Value);

internal sealed record RuntimeInput(
    string Type,
    int? QueryId,
    int? OptionId,
    RuntimeValue? Value
);

internal sealed record RuntimeEvent(
    string Type,
    int Sequence,
    int? QueryId = null,
    string? Name = null,
    string? Node = null,
    string? LineId = null,
    IReadOnlyList<string>? Substitutions = null,
    IReadOnlyList<CompiledArgument>? Arguments = null,
    IReadOnlyList<RuntimeOption>? Options = null,
    string? Message = null
);

internal sealed record RuntimeOption(
    int Id,
    string LineId,
    IReadOnlyList<string> Substitutions,
    bool IsAvailable
);
