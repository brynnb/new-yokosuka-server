using System.Globalization;
using System.Reflection;
using System.Text.Json;
using Google.Protobuf;
using Yarn;

namespace NewYokosuka.YarnCompiler;

internal sealed class RuntimeHost
{
    internal const string ProtocolVersion = "new-yokosuka-yarn-runtime-v1";
    private const int MaxYieldEvents = 10_000;
    private readonly Dialogue dialogue;
    private int sequence;
    private int queryId;
    private bool completed;
    private bool cancelling;

    private RuntimeHost(Dialogue dialogue)
    {
        this.dialogue = dialogue;
    }

    internal static async Task Run()
    {
        try
        {
            var line = await Console.In.ReadLineAsync()
                ?? throw new EndOfStreamException("A runtime start request is required.");
            var request = JsonSerializer.Deserialize<RuntimeStartRequest>(line, CompilerHost.JsonOptions)
                ?? throw new InvalidDataException("Invalid runtime start request.");
            if (request.ProtocolVersion != ProtocolVersion)
            {
                throw new InvalidDataException($"Unsupported runtime protocol '{request.ProtocolVersion}'.");
            }
            var programBytes = Convert.FromBase64String(request.ProgramBase64);
            var program = Yarn.Program.Parser.ParseFrom(programBytes);
            var variables = new MemoryVariableStore();
            var dialogue = new Dialogue(variables);
            dialogue.SetProgram(program);
            ApplyVariables(variables, request.Variables);
            var host = new RuntimeHost(dialogue);
            host.RegisterFunctions(request.Functions);
            host.ConfigureHandlers();
            dialogue.SetNode(request.StartNode);
            do
            {
                dialogue.Continue();
            }
            while (dialogue.IsActive && !host.completed);
        }
        catch (RuntimeCancelledException)
        {
            // Cancellation is an expected terminal state and has already been emitted.
        }
        catch (Exception exception)
        {
            WriteEvent(new RuntimeEvent("error", 0, Message: exception.Message));
            Console.Error.WriteLine(exception);
            Environment.ExitCode = 1;
        }
    }

    private static void ApplyVariables(MemoryVariableStore storage, IEnumerable<RuntimeVariable>? variables)
    {
        foreach (var variable in variables ?? [])
        {
            switch (variable.Value.Type)
            {
                case "string": storage.SetValue(variable.Name, variable.Value.Value); break;
                case "number": storage.SetValue(variable.Name, ParseNumber(variable.Value.Value)); break;
                case "bool": storage.SetValue(variable.Name, bool.Parse(variable.Value.Value)); break;
                default: throw new InvalidDataException($"Unsupported variable type '{variable.Value.Type}'.");
            }
        }
    }

    private void ConfigureHandlers()
    {
        dialogue.NodeStartHandler = node => Emit(new RuntimeEvent("nodeStart", NextSequence(), Node: node));
        dialogue.NodeCompleteHandler = node => Emit(new RuntimeEvent("nodeComplete", NextSequence(), Node: node));
        dialogue.LineHandler = line =>
        {
            Emit(new RuntimeEvent(
                "line", NextSequence(), LineId: line.ID, Substitutions: line.Substitutions));
            RequireContinue();
        };
        dialogue.CommandHandler = command =>
        {
            var parsed = StructuredCallParser.ParseCommand(command.Text);
            if (parsed.Error is not null)
            {
                throw new InvalidDataException($"Compiled command could not be parsed: {parsed.Error}");
            }
            Emit(new RuntimeEvent(
                "command", NextSequence(), Name: parsed.Name, Arguments: parsed.Arguments));
            RequireContinue();
        };
        dialogue.OptionsHandler = optionSet =>
        {
            var options = optionSet.Options.Select(option => new RuntimeOption(
                option.ID,
                option.Line.ID,
                option.Line.Substitutions,
                option.IsAvailable
            )).ToArray();
            Emit(new RuntimeEvent("options", NextSequence(), Options: options));
            var input = ReadInput();
            if (input.Type == "cancel")
            {
                Cancel();
            }
            if (input.Type != "select" || input.OptionId is null)
            {
                throw new InvalidDataException("Expected an option selection.");
            }
            dialogue.SetSelectedOption(input.OptionId.Value);
        };
        dialogue.DialogueCompleteHandler = () =>
        {
            if (cancelling)
            {
                return;
            }
            completed = true;
            Emit(new RuntimeEvent("complete", NextSequence()));
        };
    }

    private void RegisterFunctions(IEnumerable<FunctionDefinition>? definitions)
    {
        foreach (var definition in definitions ?? [])
        {
            var parameterTypes = (definition.ParameterTypes ?? []).Select(FunctionDeclarations.ClrType).ToArray();
            var returnType = FunctionDeclarations.ClrType(definition.ReturnType);
            if (parameterTypes.Length > 5)
            {
                throw new InvalidDataException($"Function '{definition.Name}' exceeds Yarn's five-parameter limit.");
            }
            var target = new RuntimeQuery(this, definition.Name);
            var method = typeof(RuntimeQuery).GetMethod(
                $"Invoke{parameterTypes.Length}", BindingFlags.Instance | BindingFlags.Public)
                ?? throw new MissingMethodException(typeof(RuntimeQuery).Name, $"Invoke{parameterTypes.Length}");
            var genericMethod = method.MakeGenericMethod(parameterTypes.Append(returnType).ToArray());
            var delegateType = System.Linq.Expressions.Expression.GetDelegateType(
                parameterTypes.Append(returnType).ToArray());
            dialogue.Library.RegisterFunction(
                definition.Name,
                Delegate.CreateDelegate(delegateType, target, genericMethod));
        }
    }

    private object Query(string name, Type returnType, object[] arguments)
    {
        var id = ++queryId;
        Emit(new RuntimeEvent(
            "query", NextSequence(), QueryId: id, Name: name,
            Arguments: arguments.Select(RuntimeArgument).ToArray()));
        var input = ReadInput();
        if (input.Type == "cancel")
        {
            Cancel();
        }
        if (input.Type != "queryResult" || input.QueryId != id || input.Value is null)
        {
            throw new InvalidDataException($"Expected result for query {id}.");
        }
        return ConvertValue(input.Value, returnType);
    }

    private void RequireContinue()
    {
        var input = ReadInput();
        if (input.Type == "cancel")
        {
            Cancel();
        }
        if (input.Type != "continue")
        {
            throw new InvalidDataException("Expected a continue response.");
        }
    }

    private void Cancel()
    {
        cancelling = true;
        dialogue.Stop();
        Emit(new RuntimeEvent("cancelled", NextSequence()));
        throw new RuntimeCancelledException();
    }

    private static RuntimeInput ReadInput()
    {
        var line = Console.In.ReadLine() ?? throw new EndOfStreamException("Runtime controller disconnected.");
        return JsonSerializer.Deserialize<RuntimeInput>(line, CompilerHost.JsonOptions)
            ?? throw new InvalidDataException("Invalid runtime controller message.");
    }

    private int NextSequence()
    {
        sequence++;
        if (sequence > MaxYieldEvents)
        {
            throw new InvalidOperationException($"Runtime exceeded {MaxYieldEvents} yielded events.");
        }
        return sequence;
    }

    private static CompiledArgument RuntimeArgument(object argument) => argument switch
    {
        string value => new CompiledArgument("string", true, value),
        float value => new CompiledArgument("number", true, value.ToString("R", CultureInfo.InvariantCulture)),
        bool value => new CompiledArgument("bool", true, value ? "true" : "false"),
        _ => throw new InvalidDataException($"Unsupported runtime argument type '{argument.GetType()}'."),
    };

    private static object ConvertValue(RuntimeValue value, Type returnType)
    {
        if (returnType == typeof(string) && value.Type == "string") return value.Value;
        if (returnType == typeof(float) && value.Type == "number") return ParseNumber(value.Value);
        if (returnType == typeof(bool) && value.Type == "bool") return bool.Parse(value.Value);
        throw new InvalidDataException($"Query returned {value.Type}, expected {returnType.Name}.");
    }

    private static float ParseNumber(string value) =>
        float.Parse(value, NumberStyles.Float, CultureInfo.InvariantCulture);

    private static void Emit(RuntimeEvent runtimeEvent) => WriteEvent(runtimeEvent);

    private static void WriteEvent(RuntimeEvent runtimeEvent)
    {
        Console.Out.WriteLine(JsonSerializer.Serialize(runtimeEvent, CompilerHost.JsonOptions));
        Console.Out.Flush();
    }

    private sealed class RuntimeQuery(RuntimeHost host, string name)
    {
        public TResult Invoke0<TResult>() => Convert<TResult>([]);
        public TResult Invoke1<T1, TResult>(T1 argument1) => Convert<TResult>([argument1]);
        public TResult Invoke2<T1, T2, TResult>(T1 argument1, T2 argument2) =>
            Convert<TResult>([argument1, argument2]);
        public TResult Invoke3<T1, T2, T3, TResult>(T1 argument1, T2 argument2, T3 argument3) =>
            Convert<TResult>([argument1, argument2, argument3]);
        public TResult Invoke4<T1, T2, T3, T4, TResult>(
            T1 argument1, T2 argument2, T3 argument3, T4 argument4) =>
            Convert<TResult>([argument1, argument2, argument3, argument4]);
        public TResult Invoke5<T1, T2, T3, T4, T5, TResult>(
            T1 argument1, T2 argument2, T3 argument3, T4 argument4, T5 argument5) =>
            Convert<TResult>([argument1, argument2, argument3, argument4, argument5]);

        private TResult Convert<TResult>(object?[] arguments) =>
            (TResult)host.Query(name, typeof(TResult), arguments!);
    }

    private sealed class RuntimeCancelledException : Exception;
}
