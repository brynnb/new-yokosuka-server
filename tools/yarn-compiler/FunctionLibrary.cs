using Yarn;
using Yarn.Compiler;

namespace NewYokosuka.YarnCompiler;

internal static class FunctionDeclarations
{
    internal static IReadOnlyList<Declaration> Create(IEnumerable<FunctionDefinition>? definitions)
    {
        var declarations = new List<Declaration>();
        foreach (var definition in definitions ?? [])
        {
            var function = new FunctionTypeBuilder().WithReturnType(YarnType(definition.ReturnType));
            foreach (var parameter in definition.ParameterTypes ?? [])
            {
                function.WithParameter(YarnType(parameter));
            }
            declarations.Add(new DeclarationBuilder()
                .WithName(definition.Name)
                .WithType(function.FunctionType)
                .WithSourceFileName(Declaration.ExternalDeclaration)
                .Declaration);
        }
        return declarations;
    }

    private static IType YarnType(string name) => name switch
    {
        "string" => Types.String,
        "number" => Types.Number,
        "bool" => Types.Boolean,
        _ => throw new InvalidDataException($"Unsupported Yarn function type '{name}'."),
    };

    internal static Type ClrType(string name) => name switch
    {
        "string" => typeof(string),
        "number" => typeof(float),
        "bool" => typeof(bool),
        _ => throw new InvalidDataException($"Unsupported Yarn function type '{name}'."),
    };
}
