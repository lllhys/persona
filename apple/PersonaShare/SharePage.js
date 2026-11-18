// Safari runs this in the page before Share › Persona opens (NSExtensionJavaScriptPreprocessingFile, #2282): the page's
// title, its address and the selected text, which the extension reads from the com.apple.property-list item
// (ShareText.page). Nothing else is read and nothing in the page changes.
var PersonaSharePage = function () {};

PersonaSharePage.prototype = {
  run: function (args) {
    var selection = "";
    try {
      selection = window.getSelection ? String(window.getSelection()) : "";
    } catch (e) {}
    args.completionFunction({ title: document.title || "", url: document.URL || "", selection: selection });
  },
  finalize: function (args) {}
};

var ExtensionPreprocessingJS = new PersonaSharePage();
