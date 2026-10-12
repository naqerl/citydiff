# CityDiff

[<img src="https://img.youtube.com/vi/qv0ZWVdgE5A/maxresdefault.jpg" width="70%">](https://youtu.be/qv0ZWVdgE5A)

**Higher order** diff tool that helps humans to control architecture and agents to generate interactive tours over their changes.

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

### Generate interactive tour around changes

![a tour playing over a commit range](https://s3.naqerl.com/public/demo/01-tour.gif)

A JSON script walks one commit range: the camera flies to each package and declaration, the diff lights up, and the note says why it changed.

### Read actual diff & launch nvim

![the real git diff and nvim over the city](https://s3.naqerl.com/public/demo/02-diff-and-nvim.gif)

Search a declaration, press **D** for its real unified diff, then **E** to open that line in nvim over the city.

### Control the architecture of your code base

![the city as a map of districts and dependencies](https://s3.naqerl.com/public/demo/03-architecture.gif)

Press **y** for a bird's-eye map of every package and the arcs between them, then drill from a package down to one method and its calls.

### Multiple themes

![the theme menu switching themes over the city](https://s3.naqerl.com/public/demo/04-themes.gif)

Type `/skin` to open the theme menu over the city; arrows and letters preview each theme in place, and **Enter** applies it.
