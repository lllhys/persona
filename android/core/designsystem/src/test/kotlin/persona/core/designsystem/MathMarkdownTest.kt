package persona.core.designsystem

import androidx.compose.ui.geometry.Size
import androidx.compose.ui.text.PlaceholderVerticalAlign
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.unit.Density
import androidx.compose.ui.graphics.Color
import com.mikepenz.markdown.model.ImageWidth
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import persona.core.designsystem.MarkdownRenderer.Kind
import java.io.File

/** The same cases as iOS `MathMarkdownTests`, plus the fence and placeholder parts the Android renderer adds. */
class MathMarkdownTest {
    @Test fun displayBlocksBecomeMathFences() {
        assertEquals("前\n```math\na^2 + b^2 = c^2\n```\n后", MathMarkdown.rewrite("前\n$$\na^2 + b^2 = c^2\n$$\n后"))
        assertEquals("```math\n\\int_0^1 x\\,dx\n```", MathMarkdown.rewrite("\\[\n\\int_0^1 x\\,dx\n\\]"))
        assertEquals("```math\nE = mc^2\n```", MathMarkdown.rewrite("\$\$E = mc^2\$\$"))
        assertEquals("```math\nx^2\n```", MathMarkdown.rewrite("  \$x^2$  ")) // a line that is one formula
    }

    @Test fun inlineFormulasBecomeImages() {
        val out = MathMarkdown.rewrite("面积是 \$\\pi r^2$，周长 \\(2\\pi r\\)。")
        assertEquals("面积是 ![\\pi r^2](${MathMarkdown.url("\\pi r^2")})，周长 ![2\\pi r](${MathMarkdown.url("2\\pi r")})。", out)
        assertEquals("\\frac{a}{b} [x] + 1", MathMarkdown.tex(MathMarkdown.url("\\frac{a}{b} [x] + 1")))
        assertTrue(MathMarkdown.rewrite("集合 \$[0, 1]$ 上").contains("![(0, 1)]("))
        assertNull(MathMarkdown.tex("https://example.com/a.png"))
    }

    @Test fun dollarsThatAreMoney() {
        for (text in listOf("花了 \$5 和 \$10", "\$5-\$10 之间", "价格 $ 5 $", "转义 \\\$x\\\$ 不算", "a $ b", "大约 \$10 一个月：$ 左右")) {
            assertEquals(text, text, MathMarkdown.rewrite(text))
        }
    }

    @Test fun moneyThenAFormula() {
        assertEquals("大约 \$10 一个月，公式是 ![x^2](${MathMarkdown.url("x^2")})。", MathMarkdown.rewrite("大约 \$10 一个月，公式是 \$x^2$。"))
        assertTrue(MathMarkdown.rewrite("单位 \$\\text{米}$ 可以").contains("persona-math:"))
    }

    @Test fun codeIsLeftAlone() {
        val md = "用 `\$x\$` 写公式\n```\n$$\nnot math\n$$\n```\n    \$y$ 缩进的代码"
        assertEquals(md, MathMarkdown.rewrite(md))
        assertEquals("没有公式的文字", MathMarkdown.rewrite("没有公式的文字"))
    }

    @Test fun unclosedBlockStaysAsItWas() {
        assertEquals("$$\na + b\n还没写完", MathMarkdown.rewrite("$$\na + b\n还没写完"))
    }

    @Test fun fencesDrawnAsPictures() {
        assertEquals(Kind.DisplayMath, renderedFence("math"))
        assertEquals(Kind.DisplayMath, renderedFence("LaTeX"))
        assertEquals(Kind.Mermaid, renderedFence("mermaid"))
        assertNull(renderedFence("swift"))
        assertNull(renderedFence(""))
        assertEquals(Fence("mermaid", "graph TD\n  A --> B"), Fence.of("```mermaid\ngraph TD\n  A --> B\n```"))
        assertEquals(Fence("math", "x"), Fence.of("~~~ math\nx")) // not closed yet (streaming)
    }

    @Test fun mermaidLoadsWithTheFirstDiagramOnly() {
        val render = generateSequence(File("").absoluteFile) { it.parentFile }
            .map { File(it, "core/designsystem/src/main/assets/render") }.first { it.isDirectory }
        // Formulas never wait for its 5.5 MB: the page has no script tag for it, renderMermaid adds one before drawing.
        assertFalse(Regex("<script[^>]*mermaid").containsMatchIn(File(render, "render.html").readText()))
        val js = File(render, "render.js").readText()
        assertTrue(js.contains("script.src = \"mermaid.min.js\""))
        assertTrue(Regex("renderMermaid = async [^{]*\\{\\s*await loadMermaid\\(\\);").containsMatchIn(js))
    }

    @Test fun pictureKeys() {
        val a = MarkdownRenderer.key(Kind.Mermaid, "graph TD; A-->B", dark = true, points = 17f, density = 3f)
        assertNotEquals(a, MarkdownRenderer.key(Kind.Mermaid, "graph TD; A-->B", dark = false, points = 17f, density = 3f))
        // A formula is tinted: one picture for both appearances.
        assertEquals(MarkdownRenderer.key(Kind.DisplayMath, "x", true, 17f, 3f), MarkdownRenderer.key(Kind.DisplayMath, "x", false, 17f, 3f))
        assertNotEquals(MarkdownRenderer.key(Kind.DisplayMath, "x", false, 17f, 3f), MarkdownRenderer.key(Kind.InlineMath, "x", false, 17f, 3f))
        assertNotEquals(MarkdownRenderer.key(Kind.InlineMath, "x", false, 17f, 3f), MarkdownRenderer.key(Kind.InlineMath, "x", false, 20f, 3f))
    }

    @Test fun inlineFormulasSitOnTheLineAtTheirOwnSize() {
        val images = MathImages(Color.Black, TextStyle.Default)
        val link = MathMarkdown.url("x^2")
        val density = Density(2f)
        // Not drawn yet: no room taken (the library's default is a 200dp square).
        assertEquals(Size(1f, 1f), images.placeholderConfig(link, density, Size(800f, 2000f), ImageWidth.IMAGE_WIDTH, Size.Unspecified).size)
        val drawn = images.placeholderConfig(link, density, Size(800f, 2000f), ImageWidth.IMAGE_WIDTH, Size(60f, 40f))
        assertEquals(Size(30f, 20f), drawn.size)
        assertEquals(PlaceholderVerticalAlign.TextCenter, drawn.verticalAlign)
    }
}
