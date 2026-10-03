entity "Artist" {
  key = ["ArtistId"]
  property "ArtistId" { type = "int" }
  property "Name" { type = "string" }
}

entity "Album" {
  key = ["AlbumId"]
  property "AlbumId" { type = "int" }
  property "Title" { type = "string" }
  property "ArtistId" { entity = "Artist" }
}
component "valueOf" {
  field "x" { type = "int" }
}
