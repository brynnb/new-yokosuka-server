using System.Globalization;
using Yarn.Compiler;

namespace NewYokosuka.YarnCompiler;

internal sealed record ParsedCommand(
    string Name,
    string? Error,
    IReadOnlyList<CompiledArgument> Arguments
);

internal static class StructuredCallParser
{
    internal static ParsedCommand ParseCommand(string source)
    {
        var parsed = StructuredCommandParser.ParseStructuredCommand(source);
        var errors = parsed.diagnostics
            .Where(diagnostic => diagnostic.Severity == Diagnostic.DiagnosticSeverity.Error)
            .Select(diagnostic => diagnostic.Message)
            .ToArray();
        return new ParsedCommand(
            parsed.context?.command_id?.Text ?? string.Empty,
            errors.Length == 0 ? null : string.Join("; ", errors),
            parsed.context?.structured_command_value().Select(CommandArgument).ToArray() ?? []
        );
    }

    internal static CompiledArgument ExpressionArgument(YarnSpinnerParser.ExpressionContext? expression)
    {
        if (expression is YarnSpinnerParser.ExpNegativeContext negative
            && negative.expression() is YarnSpinnerParser.ExpValueContext negativeValue
            && negativeValue.value() is YarnSpinnerParser.ValueNumberContext negativeNumber)
        {
            return new CompiledArgument("number", true, "-" + negativeNumber.GetText());
        }
        if (expression is not YarnSpinnerParser.ExpValueContext valueExpression)
        {
            return DynamicArgument(expression);
        }
        return valueExpression.value() switch
        {
            YarnSpinnerParser.ValueStringContext value => new CompiledArgument(
                "string", true, value.STRING().GetText().Trim('"')),
            YarnSpinnerParser.ValueNumberContext value => new CompiledArgument(
                "number", true, ParseNumber(value.GetText())),
            YarnSpinnerParser.ValueTrueContext => new CompiledArgument("bool", true, "true"),
            YarnSpinnerParser.ValueFalseContext => new CompiledArgument("bool", true, "false"),
            _ => DynamicArgument(expression),
        };
    }

    private static CompiledArgument CommandArgument(YarnSpinnerParser.Structured_command_valueContext context)
    {
        if (context.FUNC_ID() is { } bareValue)
        {
            return new CompiledArgument("bare", true, bareValue.GetText());
        }
        return ExpressionArgument(context.expression());
    }

    private static string ParseNumber(string source)
    {
        return float.TryParse(source, NumberStyles.Float, CultureInfo.InvariantCulture, out var value)
            ? value.ToString("R", CultureInfo.InvariantCulture)
            : source;
    }

    private static CompiledArgument DynamicArgument(YarnSpinnerParser.ExpressionContext? expression)
    {
        var type = expression?.Type switch
        {
            var yarnType when yarnType == Yarn.Types.String => "string",
            var yarnType when yarnType == Yarn.Types.Number => "number",
            var yarnType when yarnType == Yarn.Types.Boolean => "bool",
            _ => "unknown",
        };
        return new CompiledArgument(type, false, null);
    }
}
