// Open XML SDK validation for tools/conformance/ooxml-conformance.py.
//
// Copied from docxdriver scripts/openxml-validator/Program.cs (docxdriver git
// 1ffb0d7afed108a36a25c4a95b489d0fbf7a58bb); changed to open .pptx and .xlsx
// as well as .docx, by the path's extension.
//
// Reads one .docx, .pptx or .xlsx path per stdin line and writes one JSON line per path:
// {"path": ..., "errors": [{"part", "id", "description", "xpath"}]} or
// {"path": ..., "fatal": ...} when the SDK cannot open the package. Unlike the
// ECMA-376 XSDs, the SDK validates the Office extension namespaces (w14, w15,
// w16cid, w16cex, ...) and a set of semantic constraints, against the
// Microsoft 365 file format.
using System.Text.Json;
using DocumentFormat.OpenXml;
using DocumentFormat.OpenXml.Packaging;
using DocumentFormat.OpenXml.Validation;

var validator = new OpenXmlValidator(FileFormatVersions.Microsoft365) { MaxNumberOfErrors = 5000 };
string? path;
while ((path = Console.In.ReadLine()) != null)
{
    if (path.Length == 0) continue;
    object result;
    try
    {
        using OpenXmlPackage document = Path.GetExtension(path).ToLowerInvariant() switch
        {
            ".pptx" or ".potx" or ".ppsx" => PresentationDocument.Open(path, false),
            ".xlsx" => SpreadsheetDocument.Open(path, false),
            _ => WordprocessingDocument.Open(path, false),
        };
        var errors = validator.Validate(document).Select(error => new
        {
            part = error.Part?.Uri.ToString() ?? "",
            id = error.Id ?? "",
            description = error.Description ?? "",
            xpath = error.Path?.XPath ?? "",
        }).ToList();
        result = new { path, errors };
    }
    catch (Exception exception)
    {
        result = new { path, fatal = $"{exception.GetType().Name}: {exception.Message}" };
    }
    Console.Out.WriteLine(JsonSerializer.Serialize(result));
    Console.Out.Flush();
}
