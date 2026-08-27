using NewYokosuka.YarnCompiler;

if (args.Length == 1 && args[0] == "--runtime")
{
    await RuntimeHost.Run();
}
else if (args.Length == 0)
{
    await CompilerHost.Run();
}
else
{
    Console.Error.WriteLine("Usage: NewYokosuka.YarnCompiler [--runtime]");
    Environment.ExitCode = 2;
}
