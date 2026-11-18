import { Theme } from '@kit.ArkUI';
import { MarkdownBlock, MarkdownInline, MarkdownTableRow, parseMarkdown } from '../core/Markdown';
import { Tokens } from '../design/Tokens';

@ComponentV2
export struct MarkdownText {
  @Require @Param text: string;
  @Param fitContent: boolean = false;
  @Event onLink: (url: string) => void = (_url: string) => {};
  @Local blocks: MarkdownBlock[] = [];
  @Local foreground: ResourceColor = $r('sys.color.font_primary');
  @Local secondary: ResourceColor = $r('sys.color.font_secondary');
  @Local emphasize: ResourceColor = $r('sys.color.font_emphasize');
  @Local codeFill: ResourceColor = $r('sys.color.comp_background_tertiary');
  onWillApplyTheme(theme: Theme): void {
    this.foreground = theme.colors.fontPrimary; this.secondary = theme.colors.fontSecondary;
    this.emphasize = theme.colors.fontEmphasize; this.codeFill = theme.colors.compBackgroundTertiary;
  }

  aboutToAppear(): void { this.blocks = parseMarkdown(this.text); }
  @Monitor('text') textChanged(): void { this.blocks = parseMarkdown(this.text); }

  private headingSize(level: number): number {
    if (level <= 1) return Tokens.markdownHeading1;
    if (level === 2) return Tokens.markdownHeading2;
    if (level === 3) return Tokens.markdownHeading3;
    if (level === 4) return Tokens.markdownHeading4;
    if (level === 5) return Tokens.markdownHeading5;
    return Tokens.body;
  }

  private tableTextAlign(table: MarkdownBlock, column: number): TextAlign {
    const alignment = table.alignments[column] || 'start';
    if (alignment === 'center') return TextAlign.Center;
    if (alignment === 'end') return TextAlign.End;
    return TextAlign.Start;
  }

  @Builder inlineText(nodes: MarkdownInline[], size: number = Tokens.body, bold: boolean = false, alignment: TextAlign = TextAlign.Start, breakAll: boolean = false) {
    Text() {
      ForEach(nodes, (node: MarkdownInline) => this.inlineNode(node, bold), (node: MarkdownInline, index: number) => String(index))
    }
      .fontSize(size).fontColor(this.foreground).fontWeight(bold ? FontWeight.Bold : FontWeight.Normal)
      .textAlign(alignment).wordBreak(breakAll ? WordBreak.BREAK_ALL : WordBreak.BREAK_WORD)
      .copyOption(CopyOptions.LocalDevice).width(this.fitContent ? undefined : '100%')
  }

  @Builder inlineNode(node: MarkdownInline, bold: boolean = false, italic: boolean = false, code: boolean = false, url: string = '') {
    if (node.kind === 'bold') {
      ForEach(node.children, (child: MarkdownInline) => this.inlineNode(child, true, italic, code, url),
        (child: MarkdownInline, index: number) => String(index))
    } else if (node.kind === 'italic') {
      ForEach(node.children, (child: MarkdownInline) => this.inlineNode(child, bold, true, code, url),
        (child: MarkdownInline, index: number) => String(index))
    } else if (node.kind === 'code') {
      this.inlineLeaf(node.text, bold, italic, true, url)
    } else if (node.kind === 'link') {
      ForEach(node.children, (child: MarkdownInline) => this.inlineNode(child, bold, italic, code, node.url),
        (child: MarkdownInline, index: number) => String(index))
    } else {
      this.inlineLeaf(node.text, bold, italic, code, url)
    }
  }

  @Builder inlineLeaf(value: string, bold: boolean, italic: boolean, code: boolean, url: string) {
    if (code) {
      if (url) {
        Span(value).fontFamily('monospace').fontSize(Tokens.meta).fontColor(this.emphasize)
          .fontWeight(bold ? FontWeight.Bold : FontWeight.Normal).fontStyle(italic ? FontStyle.Italic : FontStyle.Normal)
          .textBackgroundStyle({ color: this.codeFill, radius: Tokens.fine })
          .decoration({ type: TextDecorationType.Underline, color: this.emphasize })
          .onClick(() => this.onLink(url))
      } else {
        Span(value).fontFamily('monospace').fontSize(Tokens.meta)
          .fontWeight(bold ? FontWeight.Bold : FontWeight.Normal).fontStyle(italic ? FontStyle.Italic : FontStyle.Normal)
          .fontColor(this.secondary)
          .textBackgroundStyle({ color: this.codeFill, radius: Tokens.fine })
      }
    } else if (url) {
      Span(value).fontColor(this.emphasize)
        .fontWeight(bold ? FontWeight.Bold : FontWeight.Normal).fontStyle(italic ? FontStyle.Italic : FontStyle.Normal)
        .decoration({ type: TextDecorationType.Underline, color: this.emphasize })
        .onClick(() => this.onLink(url))
    } else {
      Span(value).fontColor(this.foreground).fontWeight(bold ? FontWeight.Bold : FontWeight.Normal).fontStyle(italic ? FontStyle.Italic : FontStyle.Normal)
    }
  }

  @Builder blockView(block: MarkdownBlock) {
    if (block.kind === 'paragraph') {
      this.inlineText(block.inlines)
    } else if (block.kind === 'heading') {
      this.inlineText(block.inlines, this.headingSize(block.level), true)
    } else if (block.kind === 'code') {
      Column({ space: Tokens.gap }) {
        if (block.language) Text(block.language).fontSize(Tokens.meta).fontColor(this.secondary)
        Scroll() {
          Text(block.text).fontFamily('monospace').fontSize(Tokens.meta).wordBreak(WordBreak.BREAK_ALL)
            .copyOption(CopyOptions.LocalDevice).fontColor(this.foreground)
        }.scrollable(ScrollDirection.Horizontal).align(Alignment.TopStart).scrollBar(BarState.Auto).width('100%')
      }
        .padding(Tokens.inset).width('100%').alignItems(HorizontalAlign.Start)
        .backgroundColor(this.codeFill)
        .border({ width: Tokens.hairline, color: this.secondary, radius: Tokens.fine })
    } else if (block.kind === 'raw') {
      Text(block.text).fontSize(Tokens.body).fontColor(this.foreground).wordBreak(WordBreak.BREAK_WORD)
        .copyOption(CopyOptions.LocalDevice).width('100%')
    } else if (block.kind === 'rule') {
      Divider().strokeWidth(Tokens.hairline).color(this.secondary).width('100%')
        .margin({ top: Tokens.gap, bottom: Tokens.gap })
    } else if (block.kind === 'quote') {
      Column({ space: Tokens.gap }) {
        ForEach(block.children, (child: MarkdownBlock) => this.blockView(child),
          (child: MarkdownBlock, index: number) => 'quote.' + String(index))
      }
        .padding(Tokens.inset).width('100%').alignItems(HorizontalAlign.Start)
        .backgroundColor(this.codeFill)
        .border({ width: Tokens.hairline, color: this.emphasize, radius: Tokens.fine })
    } else if (block.kind === 'list') {
      Column({ space: Tokens.gap }) {
        ForEach(block.items, (item: MarkdownBlock) => this.listItem(item),
          (item: MarkdownBlock, index: number) => 'list.' + String(index))
      }.width('100%').alignItems(HorizontalAlign.Start)
    } else if (block.kind === 'table') {
      Scroll() {
        Column({ space: Tokens.fine }) {
          ForEach(block.rows, (row: MarkdownTableRow) => this.tableRow(row, block),
            (row: MarkdownTableRow, index: number) => 'row.' + String(index))
        }
      }
        .scrollable(ScrollDirection.Horizontal).align(Alignment.TopStart).scrollBar(BarState.Auto).width('100%')
    }
  }

  @Builder listItem(item: MarkdownBlock) {
    Row({ space: Tokens.inset }) {
      Text(item.marker).fontSize(Tokens.body).fontColor(this.secondary)
      Column({ space: Tokens.gap }) {
        ForEach(item.children, (child: MarkdownBlock) => this.blockView(child),
          (child: MarkdownBlock, index: number) => 'item.' + String(index))
      }.layoutWeight(1).alignItems(HorizontalAlign.Start)
    }.width('100%').alignItems(VerticalAlign.Top)
  }

  @Builder tableRow(row: MarkdownTableRow, table: MarkdownBlock) {
    Row() {
      ForEach(row.cells, (cell: MarkdownInline[], index: number) => this.tableCell(cell, table, index, row.header),
        (cell: MarkdownInline[], index: number) => 'cell.' + String(index))
    }.alignItems(VerticalAlign.Top)
  }

  @Builder tableCell(cell: MarkdownInline[], table: MarkdownBlock, index: number, header: boolean) {
    Column() {
      this.inlineText(cell, Tokens.body, header, this.tableTextAlign(table, index), true)
    }
      .width(Tokens.markdownTableCell).padding(Tokens.inset).alignItems(HorizontalAlign.Start)
      .border({ width: Tokens.hairline, color: this.secondary, radius: Tokens.fine })
      .backgroundColor(header ? this.codeFill : Color.Transparent)
  }

  build() {
    Column({ space: Tokens.gap }) {
      ForEach(this.blocks, (block: MarkdownBlock) => this.blockView(block),
        (block: MarkdownBlock, index: number) => 'block.' + String(index) + ':' + JSON.stringify(block))
    }.width(this.fitContent ? undefined : '100%').alignItems(HorizontalAlign.Start)
  }
}
