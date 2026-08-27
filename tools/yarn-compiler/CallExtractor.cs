using Antlr4.Runtime;
using Antlr4.Runtime.Misc;
using Antlr4.Runtime.Tree;
using Yarn.Compiler;

namespace NewYokosuka.YarnCompiler;

internal sealed class CallExtractor : YarnSpinnerParserBaseVisitor<object?>
{
    private readonly string fileName;
    private readonly List<CompiledCall> calls = [];
    private string currentNode = string.Empty;

    private CallExtractor(string fileName)
    {
        this.fileName = fileName;
    }

    internal static IReadOnlyList<CompiledCall> Extract(IEnumerable<FileParseResult>? parseResults)
    {
        var calls = new List<CompiledCall>();
        foreach (var parseResult in parseResults ?? [])
        {
            var extractor = new CallExtractor(parseResult.FileName);
            extractor.Visit(parseResult.Tree);
            calls.AddRange(extractor.calls);
        }
        return calls;
    }

    public override object? VisitNode([NotNull] YarnSpinnerParser.NodeContext context)
    {
        var previousNode = currentNode;
        currentNode = context.NodeTitle ?? string.Empty;
        var result = base.VisitNode(context);
        currentNode = previousNode;
        return result;
    }

    public override object? VisitCommand_statement([NotNull] YarnSpinnerParser.Command_statementContext context)
    {
        var commandText = context.command_formatted_text()?.GetText() ?? string.Empty;
        var parsed = StructuredCallParser.ParseCommand(commandText);
        calls.Add(Call(
            "command",
            parsed.Name,
            context,
            parsed.Error,
            parsed.Arguments
        ));
        return base.VisitCommand_statement(context);
    }

    public override object? VisitFunction_call([NotNull] YarnSpinnerParser.Function_callContext context)
    {
        calls.Add(Call(
            "function",
            context.FUNC_ID()?.GetText() ?? string.Empty,
            context,
            null,
            context.expression().Select(StructuredCallParser.ExpressionArgument).ToArray()
        ));
        return base.VisitFunction_call(context);
    }

    private CompiledCall Call(
        string kind,
        string name,
        ParserRuleContext context,
        string? parseError,
        IReadOnlyList<CompiledArgument> arguments)
    {
        var range = Utility.GetRange(context);
        return new CompiledCall(
            kind,
            name,
            currentNode,
            fileName,
            range.Start.Line,
            range.Start.Character,
            range.End.Line,
            range.End.Character,
            parseError,
            arguments
        );
    }

}
