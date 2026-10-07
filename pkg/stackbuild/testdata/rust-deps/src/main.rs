fn main() {
    let mut buf = itoa::Buffer::new();
    println!("greeting number {}", buf.format(1));
}
