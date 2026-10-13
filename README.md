# CityDiff

![a citydiff tour playing over a commit range](https://s3.naqerl.com/public/demo/01-tour.gif?v=3)

**Higher order** diff tool that helps humans to control architecture and agents to generate interactive tours over their changes.

### Try it live

No install: **[citydiff.naqerl.com](https://citydiff.naqerl.com/o/e8552127-fa97-4fa5-ae0d-c8bb4e8fac76/)** runs this viewer over citydiff's own source, from a hub.

## What

The city is the code. Packages are buildings; a package's sub-packages stand on its roof.
A **type** is a wide pale block holding its methods. A **function** or **method** is a tower
whose **height is its body size**. A **call** is an arc from the top of the caller to
the top of what it calls.

## Install

Just ask your agent to install and chill

```
Install citydiff, read added skills and create a demo tour on whatever project I'm currently working on. https://raw.githubusercontent.com/naqerl/citydiff/refs/heads/main/install.sh
```

## Showcases

### Read actual diff & launch nvim

![the real git diff and nvim over the city](https://s3.naqerl.com/public/demo/02-diff-and-nvim.gif?v=3)

Search a declaration, press **D** for its real unified diff, then **E** to open that line in nvim over the city.

### Control the architecture of your code base

![the city as a map of districts and dependencies](https://s3.naqerl.com/public/demo/03-architecture.gif?v=3)

Press **y** for a bird's-eye map of every package and the arcs between them, then drill from a package down to one method and its calls.

### Multiple themes

![the theme menu switching themes over the city](https://s3.naqerl.com/public/demo/04-themes.gif?v=3)

Type `/skin` to open the theme menu over the city; arrows and letters preview each theme in place, and **Enter** applies it.

## License

Apache-2.0. See [LICENSE](LICENSE).
